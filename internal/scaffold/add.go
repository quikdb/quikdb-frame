package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/quikdb/quikdb-frame/internal/project"
)

func Add(svcType, svcName string) error {
	// Verify we're in a quikdb-frame project
	if _, err := os.Stat("quikdb.yaml"); os.IsNotExist(err) {
		return fmt.Errorf("quikdb.yaml not found. Are you in a quikdb-frame project?")
	}

	manifest, err := project.Load("quikdb.yaml")
	if err != nil {
		return err
	}
	if err := project.ValidateGoModule("go.mod", manifest.GoModule); err != nil {
		return fmt.Errorf("cannot add a shared-module service: %w", err)
	}
	for name, service := range manifest.Services {
		if _, err := os.Lstat(filepath.Join(filepath.FromSlash(service.Path), "go.mod")); err == nil {
			return fmt.Errorf("cannot add a shared-module service while %s has a nested go.mod", name)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect service %s module boundary: %w", name, err)
		}
	}
	fullName, err := manifest.AddService(svcType, svcName)
	if err != nil {
		return err
	}
	svcDir := filepath.Join("services", fullName)

	if _, err := os.Stat(svcDir); err == nil {
		return fmt.Errorf("service %s already exists", fullName)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect service %s: %w", fullName, err)
	}

	switch svcType {
	case "api":
		err = addAPI(svcDir, svcName, fullName, manifest.GoModule, manifest.Services[fullName].Port)
	case "ws":
		err = addWS(svcDir, svcName, fullName, manifest.GoModule, manifest.Services[fullName].Port)
	case "worker":
		err = addWorker(svcDir, svcName, fullName, manifest.GoModule)
	case "web":
		err = addWeb(svcDir, svcName, fullName, manifest.GoModule, manifest.Services[fullName].Port)
	}
	if err != nil {
		_ = os.RemoveAll(svcDir)
		return err
	}
	if err := project.Save("quikdb.yaml", manifest); err != nil {
		_ = os.RemoveAll(svcDir)
		return err
	}

	fmt.Printf("Added service: %s\n", fullName)
	fmt.Printf("  Location: %s/\n", svcDir)
	fmt.Println()
	fmt.Println("Next: add routes, then run:")
	fmt.Printf("  quikdb-frame dev %s\n", fullName)
	fmt.Println()
	return nil
}

func addAPI(svcDir, name, fullName, module string, port int) error {
	os.MkdirAll(filepath.Join(svcDir, "handlers"), 0755)

	files := map[string]string{
		"main.go": fmt.Sprintf(`package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"context"
	"time"

	"%s/shared/logging"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "%d"
	}

	mux := http.NewServeMux()
	registerRoutes(mux)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      logging.RequestLogger(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan
		logging.Info("shutdown requested")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	logging.Info(fmt.Sprintf("%s listening on port %%s", port))
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logging.Error("api server stopped unexpectedly")
		os.Exit(1)
	}
}
`, module, port, fullName),
		"routes.go": fmt.Sprintf(`package main

import (
	"encoding/json"
	"net/http"
	"time"

	"%s/shared/auth"
	"%s/shared/db"
)

var startTime = time.Now()

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"service": "%s",
			"database": db.Status(),
			"uptime":  time.Since(startTime).String(),
		})
	})
	mux.Handle("GET /api/me", auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"userId": auth.GetUserID(r)})
	})))

	// Add your %s routes here
}
`, module, module, fullName, name),
		"Dockerfile": goServiceDockerfile(svcDir, port, true),
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

func addWS(svcDir, name, fullName, module string, port int) error {
	os.MkdirAll(filepath.Join(svcDir, "handlers"), 0755)

	files := map[string]string{
		"main.go": fmt.Sprintf(`package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"context"
	"time"

	"%s/shared/auth"
	"%s/shared/logging"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "%d"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`+"`"+`{"status":"ok","service":"%s"}`+"`"+`))
	})

	mux.Handle("GET /ws", auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrade handler
		// TODO: implement with nhooyr.io/websocket
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`+"`"+`{"error":"websocket not yet implemented"}`+"`"+`))
	})))

	server := &http.Server{
		Addr:    ":" + port,
		Handler: logging.RequestLogger(mux),
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	logging.Info(fmt.Sprintf("%s listening on port %%s", port))
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logging.Error("websocket server stopped unexpectedly")
		os.Exit(1)
	}
}
`, module, module, port, fullName, fullName),
		"Dockerfile": goServiceDockerfile(svcDir, port, true),
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

func addWorker(svcDir, name, fullName, module string) error {
	os.MkdirAll(svcDir, 0755)

	files := map[string]string{
		"main.go": fmt.Sprintf(`package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"%s/shared/db"
	"%s/shared/logging"
)

func main() {
	logging.Info("%s worker starting")

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
			logging.Info("%s worker shutting down")
			return
		case <-ticker.C:
			logging.Info("%s worker heartbeat; database=" + db.Status())
			process()
		}
	}
}

func process() {
	// TODO: implement your worker logic
	_ = os.Getenv("REDIS_URL")
}
`, module, module, fullName, fullName, fullName, fullName),
		"Dockerfile": goServiceDockerfile(svcDir, 0, false),
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

func addWeb(svcDir, name, fullName, module string, port int) error {
	os.MkdirAll(filepath.Join(svcDir, "src"), 0755)

	files := map[string]string{
		"server.go":      webServerGoWithPort(module, port),
		"index.html":     webIndexHtml(name, ""),
		"package.json":   webPackageJson(name, ""),
		"vite.config.ts": webViteConfig("", ""),
		"src/index.tsx":  webIndexTsx("", ""),
		"src/app.tsx":    webAppTsx(name, ""),
		"Dockerfile":     webServiceDockerfile(svcDir, port),
		"quikdb.json":    webQuikdbJson(name, ""),
	}

	return writeFiles(svcDir, fullName, files)
}

func writeFiles(svcDir, fullName string, files map[string]string) error {
	for path, content := range files {
		fullPath := filepath.Join(svcDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	return nil
}
