package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	staticDir := "./static"
	fs := http.FileServer(http.Dir(staticDir))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health check
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok"}`))
			return
		}

		// If the path has a file extension, serve the file directly
		if strings.Contains(r.URL.Path, ".") {
			fs.ServeHTTP(w, r)
			return
		}

		// SPA fallback — serve index.html for all non-file paths
		http.ServeFile(w, r, staticDir+"/index.html")
	})

	log.Printf("web service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}
