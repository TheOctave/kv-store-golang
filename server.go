package main

import (
	"encoding/json"
	"fmt"
	"io"
	"key-value-store/store"
	"net/http"
	"os"

	"github.com/go-chi/chi"
	hclog "github.com/hashicorp/go-hclog"
)

var (
	log = hclog.Default()
)

func main() {

	// Get port from env variables or set to 8080.
	port := "8080"
	if fromEnv := os.Getenv("PORT"); fromEnv != "" {
		port = fromEnv
	}
	log.Info(fmt.Sprintf("Starting up on localhost:%s", port))

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
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(config.Middleware)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		jw := json.NewEncoder(w)
		jw.Encode(map[string]string{"hello": "world"})
	})

	r.Post("/raft/add", config.AddHandler())
	r.Post("/key/{key}", func(w http.ResponseWriter, r *http.Request) {
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

	http.ListenAndServe(":"+port, r)
}
