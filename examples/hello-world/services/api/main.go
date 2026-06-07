package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

var startTime = time.Now()

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"version": "1.0.0",
			"uptime":  time.Since(startTime).String(),
		})
	})

	// Example endpoint
	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Hello from quikdb-frame",
		})
	})

	// List tasks (example CRUD)
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "1", "title": "Build with quikdb-frame", "done": false},
			{"id": "2", "title": "Deploy to QuikDB Compute", "done": false},
			{"id": "3", "title": "Post about it with #BuildQuik", "done": false},
		})
	})

	log.Printf("api service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
