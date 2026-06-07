package convert

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func Run(srcPath, framework string) error {
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("source path %s does not exist", srcPath)
	}

	switch framework {
	case "express":
		return convertExpress(srcPath)
	case "nestjs", "nextjs", "fastapi", "django", "flask", "gin", "fiber", "spring":
		return fmt.Errorf("%s converter is not yet implemented. Contributions welcome: https://github.com/quikdb/quikdb-frame/blob/main/CONTRIBUTING.md", framework)
	default:
		return fmt.Errorf("unknown framework: %s. Supported: express, nestjs, nextjs, fastapi, django, flask", framework)
	}
}

func convertExpress(srcPath string) error {
	fmt.Println("Scanning Express project...")
	fmt.Println()

	scan := scanExpress(srcPath)

	fmt.Printf("Found: %d routes, %d middleware, %d models, %d env vars\n",
		len(scan.routes), len(scan.middleware), len(scan.models), len(scan.envVars))
	fmt.Println()

	// Generate output
	outPath := srcPath + "-quikdb"
	if err := generateFromScan(outPath, scan); err != nil {
		return err
	}

	fmt.Println("Converting...")
	fmt.Printf("Done. Output: %s/\n\n", outPath)

	fmt.Println("Generated:")
	fmt.Printf("  %s/\n", outPath)
	fmt.Println("  ├── quikdb.yaml")
	fmt.Println("  ├── shared/")
	fmt.Println("  ├── services/")
	fmt.Println("  │   ├── api/          (Go REST API)")
	fmt.Println("  │   └── web/          (Preact + Go file server)")
	fmt.Println("  └── CLAUDE.md")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", outPath)
	fmt.Println("  # Review generated Go code")
	fmt.Println("  # Add your business logic to the handler stubs")
	fmt.Println("  quikdb-frame dev")
	fmt.Println()

	return nil
}

type scanResult struct {
	routes     []routeInfo
	middleware []string
	models     []string
	envVars    []string
	hasWS      bool
	hasStatic  bool
	dbType     string
}

type routeInfo struct {
	method string
	path   string
	file   string
}

func scanExpress(srcPath string) scanResult {
	result := scanResult{}

	// Scan for routes
	routePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:app|router)\.(get|post|put|patch|delete)\s*\(\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`@(Get|Post|Put|Patch|Delete)\s*\(\s*['"]([^'"]*)['"]\s*\)`),
	}

	middlewarePatterns := []*regexp.Regexp{
		regexp.MustCompile(`app\.use\s*\(\s*(\w+)`),
		regexp.MustCompile(`(?:cors|helmet|morgan|bodyParser|express\.json|express\.urlencoded|cookieParser|session|passport)`),
	}

	// Walk source files
	filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".git" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".js" && ext != ".ts" && ext != ".tsx" && ext != ".jsx" && ext != ".mjs" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		relPath, _ := filepath.Rel(srcPath, path)

		// Find routes
		for _, pattern := range routePatterns {
			matches := pattern.FindAllStringSubmatch(content, -1)
			for _, m := range matches {
				result.routes = append(result.routes, routeInfo{
					method: strings.ToUpper(m[1]),
					path:   m[2],
					file:   relPath,
				})
			}
		}

		// Find middleware
		for _, pattern := range middlewarePatterns {
			matches := pattern.FindAllStringSubmatch(content, -1)
			for _, m := range matches {
				mw := m[0]
				if len(m) > 1 {
					mw = m[1]
				}
				if !contains(result.middleware, mw) {
					result.middleware = append(result.middleware, mw)
				}
			}
		}

		// Detect WebSocket
		if strings.Contains(content, "socket.io") || strings.Contains(content, "ws") && strings.Contains(content, "WebSocket") {
			result.hasWS = true
		}

		// Detect database
		if strings.Contains(content, "mongoose") || strings.Contains(content, "mongodb") {
			result.dbType = "mongo"
		} else if strings.Contains(content, "prisma") || strings.Contains(content, "sequelize") || strings.Contains(content, "pg") {
			result.dbType = "postgres"
		}

		// Detect models
		if strings.Contains(relPath, "model") || strings.Contains(relPath, "schema") {
			modelName := strings.TrimSuffix(filepath.Base(relPath), ext)
			modelName = strings.TrimSuffix(modelName, ".model")
			modelName = strings.TrimSuffix(modelName, ".schema")
			if modelName != "" && !contains(result.models, modelName) {
				result.models = append(result.models, modelName)
			}
		}

		return nil
	})

	// Scan .env or .env.example
	for _, envFile := range []string{".env.example", ".env", ".env.sample"} {
		envPath := filepath.Join(srcPath, envFile)
		if data, err := os.ReadFile(envPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) >= 1 && !contains(result.envVars, parts[0]) {
					result.envVars = append(result.envVars, parts[0])
				}
			}
			break
		}
	}

	// Check for static files
	for _, dir := range []string{"public", "static", "assets"} {
		if _, err := os.Stat(filepath.Join(srcPath, dir)); err == nil {
			result.hasStatic = true
			break
		}
	}

	// Check package.json for more info
	pkgPath := filepath.Join(srcPath, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		var pkg map[string]interface{}
		if json.Unmarshal(data, &pkg) == nil {
			if deps, ok := pkg["dependencies"].(map[string]interface{}); ok {
				for dep := range deps {
					if dep == "socket.io" || dep == "ws" {
						result.hasWS = true
					}
					if dep == "mongoose" || dep == "mongodb" {
						result.dbType = "mongo"
					}
					if dep == "pg" || dep == "prisma" || dep == "@prisma/client" {
						result.dbType = "postgres"
					}
				}
			}
		}
	}

	if result.dbType == "" {
		result.dbType = "postgres"
	}

	return result
}

func generateFromScan(outPath string, scan scanResult) error {
	// Create output directory structure
	dirs := []string{
		"shared/db",
		"shared/auth",
		"shared/types",
		"shared/logging",
		"services/api/handlers",
		"services/web/src",
		"config",
	}

	for _, dir := range dirs {
		os.MkdirAll(filepath.Join(outPath, dir), 0755)
	}

	// Generate quikdb.yaml
	name := filepath.Base(outPath)
	name = strings.TrimSuffix(name, "-quikdb")

	yaml := fmt.Sprintf(`name: %s
version: 1.0.0

database:
  primary:
    type: %s

services:
  api:
    type: api
    path: services/api
    port: 8080
    routes:
      - /api/*
    env:
      - DATABASE_URL
      - REDIS_URL
      - JWT_SECRET
      - PORT

  web:
    type: web
    path: services/web
    port: 3000
    routes:
      - /*
    env:
      - API_URL
      - PORT

routing:
  rules:
    - path: /api/*    service: api
    - path: /*        service: web
`, name, scan.dbType)

	os.WriteFile(filepath.Join(outPath, "quikdb.yaml"), []byte(yaml), 0644)

	// Generate route handlers (skip health — already built in)
	routeRegistrations := ""
	for _, r := range scan.routes {
		if r.path == "/health" || r.path == "/api/health" {
			continue
		}
		handlerName := routeToHandlerName(r.method, r.path)
		routeRegistrations += fmt.Sprintf("\tmux.HandleFunc(\"%s %s\", %s)\n", r.method, r.path, handlerName)
	}

	// routes.go
	routesGo := fmt.Sprintf(`package main

import "net/http"

func registerRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /health", handleHealth)

	// Converted routes from Express
%s}
`, routeRegistrations)

	os.WriteFile(filepath.Join(outPath, "services/api/routes.go"), []byte(routesGo), 0644)

	// Generate handler stubs
	handlers := `package main

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(startTime).String(),
	})
}

`
	for _, r := range scan.routes {
		if r.path == "/health" || r.path == "/api/health" {
			continue
		}
		handlerName := routeToHandlerName(r.method, r.path)
		handlers += fmt.Sprintf(`// %s %s (from %s)
func %s(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "TODO: implement %s %s",
	})
}

`, r.method, r.path, r.file, handlerName, r.method, r.path)
	}

	os.WriteFile(filepath.Join(outPath, "services/api/handlers.go"), []byte(handlers), 0644)

	// main.go
	mainGo := fmt.Sprintf(`package main

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

	log.Printf("%s api listening on :%%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
`, name)

	os.WriteFile(filepath.Join(outPath, "services/api/main.go"), []byte(mainGo), 0644)
	os.WriteFile(filepath.Join(outPath, "services/api/go.mod"), []byte(fmt.Sprintf("module %s/services/api\n\ngo 1.23\n", name)), 0644)
	os.WriteFile(filepath.Join(outPath, "services/api/Dockerfile"), []byte(`FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app .

FROM scratch
COPY --from=builder /build/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/app"]
`), 0644)

	// .env.example
	envContent := ""
	for _, v := range scan.envVars {
		envContent += v + "=\n"
	}
	if envContent == "" {
		envContent = "DATABASE_URL=\nREDIS_URL=\nJWT_SECRET=\nPORT=8080\n"
	}
	os.WriteFile(filepath.Join(outPath, ".env.example"), []byte(envContent), 0644)

	// CLAUDE.md
	claudeMd := fmt.Sprintf(`# %s — converted to quikdb-frame

## Original framework: Express
## Converted routes: %d
## Models: %s

## Architecture
Single api service with all routes. Split into multiple services as needed.

## Strict rules
- All Go services: CGO_ENABLED=0, single static binary, scratch Docker image
- All services read PORT from environment
- GET /health returns JSON with status
- NO node_modules in production
- Graceful shutdown on SIGTERM
`, name, len(scan.routes), strings.Join(scan.models, ", "))

	os.WriteFile(filepath.Join(outPath, "CLAUDE.md"), []byte(claudeMd), 0644)

	return nil
}

func routeToHandlerName(method, path string) string {
	// /api/users/:id -> handleGetUsersById
	path = strings.ReplaceAll(path, "/", " ")
	path = strings.ReplaceAll(path, ":", "By")
	path = strings.ReplaceAll(path, "-", " ")
	path = strings.ReplaceAll(path, "_", " ")

	words := strings.Fields(path)
	name := "handle" + strings.Title(strings.ToLower(method))
	for _, w := range words {
		if w == "api" {
			continue
		}
		name += strings.Title(w)
	}

	return name
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
