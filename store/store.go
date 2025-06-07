package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"key-value-store/observability/otellib"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

type Config struct {
	raft *raft.Raft
	fsm  *fsm
}

var (
	log = hclog.Default()
)

// NewRaftSEtup configures  a raft server.
func NewRaftSetup(storagePath, host, raftPort, raftLeader string) (*Config, error) {
	cfg := &Config{}

	if err := os.MkdirAll(storagePath, os.ModePerm); err != nil {
		return nil, fmt.Errorf("setting up storagedir: %w", err)
	}

	cfg.fsm = &fsm{
		dataFile: fmt.Sprintf("%s/data.json", storagePath),
	}

	stableStore, err := raftboltdb.NewBoltStore(storagePath + "/stable")
	if err != nil {
		return nil, fmt.Errorf("building stable store: %w", err)
	}

	logStore, err := raftboltdb.NewBoltStore(storagePath + "/log")
	if err != nil {
		return nil, fmt.Errorf("building log stora")
	}

	snapshotStore, err := raft.NewFileSnapshotStoreWithLogger(storagePath+"/snaps", 5, log)
	if err != nil {
		return nil, fmt.Errorf("building snapshotstore: %w", err)
	}

	// create a TCP transport for the raft server
	fullTarget := fmt.Sprintf("%s:%s", host, raftPort)
	addr, err := net.ResolveTCPAddr("tcp", fullTarget)
	if err != nil {
		return nil, fmt.Errorf("getting address: %w", err)
	}
	trans, err := raft.NewTCPTransportWithLogger(fullTarget, addr, 10, 10*time.Second, log)
	if err != nil {
		return nil, fmt.Errorf("building transport: %w", err)
	}

	// Build the raft configuration
	raftSettings := raft.DefaultConfig()
	raftSettings.LocalID = raft.ServerID(uuid.New().URN())

	if err := raft.ValidateConfig(raftSettings); err != nil {
		return nil, fmt.Errorf("could not validate config: %w", err)
	}

	node, err := raft.NewRaft(raftSettings, cfg.fsm, logStore, stableStore, snapshotStore, trans)
	if err != nil {
		return nil, fmt.Errorf("could not create raft node: %w", err)
	}
	cfg.raft = node

	if cfg.raft.Leader() != "" {
		raftLeader = string(cfg.raft.Leader())
	}

	// Make ourselves the leader!
	if raftLeader == "" {
		raftConfig := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raftSettings.LocalID,
					Address: raft.ServerAddress(fullTarget),
				},
			},
		}

		cfg.raft.BootstrapCluster(raftConfig)
	}

	// Watch the leader election forever.
	leaderCh := cfg.raft.LeaderCh()
	go func() {
		for {
			select {
			case isLeader := <-leaderCh:
				if isLeader {
					log.Info("cluster leadership acquired")
					//snapshot at random
					chance := rand.Intn(10)
					if chance == 0 {
						cfg.raft.Snapshot()
					}
				}
			}
		}
	}()

	// We're not the leader, tell them about us.
	if raftLeader != "" {
		// Lets just chill for a bit until leader might be ready
		time.Sleep(10 * time.Second)

		postJSON := fmt.Sprintf(`{"ID": %q, "Address": %q}`, raftSettings.LocalID, fullTarget)
		resp, err := http.Post(
			raftLeader+"/raft/add",
			"application/json; charset=utf-8",
			strings.NewReader(postJSON))

		if err != nil {
			return nil, fmt.Errorf("added self to leader", "leader", raftLeader, "response", resp)
		}
	}

	return cfg, nil
}

// Sets a value for a key.
func (cfg *Config) Set(ctx context.Context, key, value string) error {
	if cfg.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	cmd, err := json.Marshal(Command{Action: "set", Key: key, Value: value})
	if err != nil {
		return fmt.Errorf("marshaling command: %w", err)
	}

	l := cfg.raft.Apply(cmd, time.Minute)
	return l.Error()
}

// Get gets the value for a key
func (cfg *Config) Get(ctx context.Context, key string) (string, error) {
	return cfg.fsm.localGet(ctx, key)
}

// Delete removes a key and its value from the store.
func (cfg *Config) Delete(ctx context.Context, key string) error {
	if cfg.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	cmd, err := json.Marshal(Command{Action: "delete", Key: key})
	if err != nil {
		return fmt.Errorf("marshaling command: %w", err)
	}

	l := cfg.raft.Apply(cmd, time.Minute)
	return l.Error()
}

// AddHandler is an http handler that responds to Add requests to join a raft cluster
func (cfg *Config) AddHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, span := otellib.Tracer.Start(ctx, "POST /add/{key}")
		defer span.End()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		jw := json.NewEncoder(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			jw.Encode(map[string]string{"error": err.Error()})
			return
		}
		log.Debug("got request", "body", string(body))

		var s *raft.Server
		if err := json.Unmarshal(body, &s); err != nil {
			log.Error("could not parse json", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			jw.Encode(map[string]string{"error": err.Error()})
			return
		}
		cfg.raft.AddVoter(s.ID, s.Address, 0, time.Minute)
		jw.Encode(map[string]string{"status": "success"})
	}
}

// Middleware passes the incoming request to the leader of the cluster
func (cfg *Config) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.raft.State() != raft.Leader {
			ldr := cfg.raft.Leader()
			if ldr == "" {
				log.Error("leader address is empty")
				h.ServeHTTP(w, r)
				return
			}

			prxy := httputil.NewSingleHostReverseProxy(RaftAddressToHTTP(ldr))
			prxy.ServeHTTP(w, r)
			return
		}

		h.ServeHTTP(w, r)
	})
}

// RaftAddressToHTTP turns a raft ServerAddress to a parsed URL.
func RaftAddressToHTTP(s raft.ServerAddress) *url.URL {
	return nil
}
