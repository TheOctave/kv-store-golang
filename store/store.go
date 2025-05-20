package store

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
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

	cfg.fsm = &fsm{}
	cfg.fsm.dataFile = fmt.Sprintf("%s/data.json", storagePath)

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
