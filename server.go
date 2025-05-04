package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi"
)

type ErrorMessage struct {
	Error string `json:"error"`
}

func main() {
	// Get port from env variables or set to 8080
	port := "8080"
	if fromEnv := os.Getenv("PORT"); fromEnv != "" {
		port = fromEnv
	}
	log.Printf("Starting up on http://localhost:%s", port)

	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, err := json.Marshal(map[string]string{"hello": "world"})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			b, _ = json.Marshal(ErrorMessage{Error: err.Error()})
		}
		w.Write(b)
	})

	log.Fatal(http.ListenAndServe(":"+port, r))
}
