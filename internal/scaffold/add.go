package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

func Add(svcType, svcName string) error {
	// Verify we're in a quikdb-frame project
	if _, err := os.Stat("quikdb.yaml"); os.IsNotExist(err) {
		return fmt.Errorf("quikdb.yaml not found. Are you in a quikdb-frame project?")
	}

	if err := validateName(svcName, "service name"); err != nil {
		return err
	}

	fullName := svcType + "-" + svcName
	if svcType == "web" {
		fullName = "web-" + svcName
	}
	svcDir := filepath.Join("services", fullName)

	if _, err := os.Stat(svcDir); err == nil {
		return fmt.Errorf("service %s already exists", fullName)
	}

	switch svcType {
	case "api":
		return addAPI(svcDir, svcName, fullName)
	case "ws":
		return addWS(svcDir, svcName, fullName)
	case "worker":
		return addWorker(svcDir, svcName, fullName)
	case "web":
		return addWeb(svcDir, svcName, fullName)
	default:
		return fmt.Errorf("unknown service type: %s (use: api, ws, worker, web)", svcType)
	}
}

func addAPI(svcDir, name, fullName string) error {
	os.MkdirAll(filepath.Join(svcDir, "handlers"), 0755)

	files := map[string]string{
		"main.go": fmt.Sprintf(`package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"context"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	registerRoutes(mux)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	log.Printf("%s listening on :%%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
`, fullName),
		"routes.go": fmt.Sprintf(`package main

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"service": "%s",
			"uptime":  time.Since(startTime).String(),
		})
	})

	// Add your %s routes here
}
`, fullName, name),
		"go.mod": fmt.Sprintf("module services/%s\n\ngo 1.23\n", fullName),
		"Dockerfile": `FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app .

FROM scratch
COPY --from=builder /build/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/app"]
`,
		"quikdb.json": fmt.Sprintf(`{
  "name": "%s",
  "type": "api",
  "buildCommand": "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o app .",
  "startCommand": "./app",
  "envVars": {}
}
`, fullName),
	}

	return writeFiles(svcDir, fullName, files)
}

func addWS(svcDir, name, fullName string) error {
	os.MkdirAll(filepath.Join(svcDir, "handlers"), 0755)

	files := map[string]string{
		"main.go": fmt.Sprintf(`package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"context"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(` + "`" + `{"status":"ok","service":"%s"}` + "`" + `))
	})

	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrade handler
		// TODO: implement with nhooyr.io/websocket
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(` + "`" + `{"error":"websocket not yet implemented"}` + "`" + `))
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	log.Printf("%s listening on :%%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
`, fullName, fullName),
		"go.mod": fmt.Sprintf("module services/%s\n\ngo 1.23\n", fullName),
		"Dockerfile": `FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app .

FROM scratch
COPY --from=builder /build/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8081
ENTRYPOINT ["/app"]
`,
		"quikdb.json": fmt.Sprintf(`{
  "name": "%s",
  "type": "ws",
  "buildCommand": "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o app .",
  "startCommand": "./app",
  "envVars": {}
}
`, fullName),
	}

	return writeFiles(svcDir, fullName, files)
}

func addWorker(svcDir, name, fullName string) error {
	os.MkdirAll(svcDir, 0755)

	files := map[string]string{
		"main.go": fmt.Sprintf(`package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.Printf("%s worker starting...")

	// TODO: Connect to Redis Stream and consume messages
	// stream := os.Getenv("REDIS_STREAM")
	// group := "%s-group"

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			log.Println("Shutting down %s worker...")
			return
		case <-ticker.C:
			log.Println("%s: heartbeat")
			process()
		}
	}
}

func process() {
	// TODO: implement your worker logic
	_ = os.Getenv("REDIS_URL")
}
`, fullName, fullName, fullName, fullName),
		"go.mod": fmt.Sprintf("module services/%s\n\ngo 1.23\n", fullName),
		"Dockerfile": `FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app .

FROM scratch
COPY --from=builder /build/app /app
ENTRYPOINT ["/app"]
`,
		"quikdb.json": fmt.Sprintf(`{
  "name": "%s",
  "type": "worker",
  "buildCommand": "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o app .",
  "startCommand": "./app",
  "envVars": {}
}
`, fullName),
	}

	return writeFiles(svcDir, fullName, files)
}

func addWeb(svcDir, name, fullName string) error {
	os.MkdirAll(filepath.Join(svcDir, "src"), 0755)

	files := map[string]string{
		"server.go":    webServerGo("", ""),
		"go.mod":       fmt.Sprintf("module services/%s\n\ngo 1.23\n", fullName),
		"index.html":   webIndexHtml(name, ""),
		"package.json": webPackageJson(name, ""),
		"vite.config.ts": webViteConfig("", ""),
		"src/index.tsx":  webIndexTsx("", ""),
		"src/app.tsx":    webAppTsx(name, ""),
		"Dockerfile":    webDockerfile("", ""),
		"quikdb.json":   webQuikdbJson(name, ""),
	}

	return writeFiles(svcDir, fullName, files)
}

func writeFiles(svcDir, fullName string, files map[string]string) error {
	for path, content := range files {
		fullPath := filepath.Join(svcDir, path)
		dir := filepath.Dir(fullPath)
		os.MkdirAll(dir, 0755)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	fmt.Printf("Added service: %s\n", fullName)
	fmt.Printf("  Location: %s/\n", svcDir)
	fmt.Println()
	fmt.Println("Next: add routes, then run:")
	fmt.Printf("  quikdb-frame dev %s\n", fullName)
	fmt.Println()

	return nil
}
