package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"key-value-store/observability/otellib"
	"key-value-store/store"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-chi/chi"
	hclog "github.com/hashicorp/go-hclog"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
)

var (
	log = hclog.Default()
)

func main() {

	// Handle SIGINIT (CTRL+C) gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Set up OpenTelemetry.
	otelShutdown, err := otellib.SetupOTelSDK(ctx)
	if err != nil {
		panic(err)
	}

	// Handle shtudown properly so nothing leaks
	defer func() {
		err = errors.Join(err, otelShutdown(ctx))
	}()

	// Get port from env variables or set to 8080.
	port := "8080"
	if fromEnv := os.Getenv("PORT"); fromEnv != "" {
		port = fromEnv
	}
	log.Info(fmt.Sprintf("Starting up on localhost:%s", port))

	config, err := setupRAFTServers()
	if err != nil {
		panic(err)
	}

	r := createRouter(config)

	// Start HTTP Server.
	server := &http.Server{
		Addr:         ":" + port,
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      r,
	}
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- server.ListenAndServe()
	}()

	// Wait for interruption.
	select {
	case err = <-srvErr:
		// Error when starting HTTP
		return
	case <-ctx.Done():
		// Wait for first CTRL+C.
		// Stop receiving signal notifications as on as possible
		stop()
	}

	// When Shutdown is called, ListenAndServe immediately returns ErrServerClosed.
	err = server.Shutdown(context.Background())
	if err != nil {
		log.Error("Error during shutdown: {}", err)
	}
}

func setupRAFTServers() (*store.Config, error) {

	storagePath := "/tmp/kv"
	if fromEnv := os.Getenv("STORAGE_PATH"); fromEnv != "" {
		storagePath = fromEnv
	}

	host := "localhost"
	if fromEnv := os.Getenv("RAFT_ADDRESS"); fromEnv != "" {
		fmt.Println(fromEnv)
		host = fromEnv
	}

	raftPort := "8081"
	if fromEnv := os.Getenv("RAFT_PORT"); fromEnv != "" {
		raftPort = fromEnv
	}

	leader := os.Getenv("RAFT_LEADER")
	config, err := store.NewRaftSetup(storagePath, host, raftPort, leader)
	if err != nil {
		log.Error("couldn't set up Raft", "error", err)
		return nil, err
	}

	return config, nil
}

func createRouter(config *store.Config) *chi.Mux {
	r := chi.NewRouter()
	r.Use(otelmux.Middleware("localhost"), config.Middleware)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, span := otellib.Tracer.Start(ctx, "GET /")
		defer span.End()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		jw := json.NewEncoder(w)
		jw.Encode(map[string]string{"hello": "world"})
	})

	r.Post("/raft/add", config.AddHandler())
	r.Post("/key/{key}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, span := otellib.Tracer.Start(ctx, "POST /key/{key}")
		defer span.End()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		jw := json.NewEncoder(w)
		key := chi.URLParam(r, "key")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			jw.Encode(map[string]string{"error": err.Error()})
			return
		}

		if err := config.Set(r.Context(), key, string(body)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			jw.Encode(map[string]string{"error": err.Error()})
			return
		}
	})

	r.Get("/key/{key}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, span := otellib.Tracer.Start(ctx, "GET /key/{key}")
		defer span.End()

		key := chi.URLParam(r, "key")

		data, err := config.Get(r.Context(), key)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Set("Content-Type", "applicatin/json; charset=utf-8")
			jw := json.NewEncoder(w)
			jw.Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Write([]byte(data))
	})

	r.Delete("/key/{key}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, span := otellib.Tracer.Start(ctx, "DELETE /key/{key}")
		defer span.End()

		key := chi.URLParam(r, "key")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		jw := json.NewEncoder(w)

		if err := config.Delete(r.Context(), key); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			jw.Encode(map[string]string{"error": err.Error()})
			return
		}

		jw.Encode(map[string]string{"status": "success"})
	})

	return r
}
