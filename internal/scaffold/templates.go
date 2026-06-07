package scaffold

import "fmt"

func quikdbYaml(name, dbType string) string {
	return fmt.Sprintf(`name: %s
version: 1.0.0

database:
  primary:
    type: %s
    migrations: shared/db/migrations/
  cache:
    type: redis

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
`, name, dbType)
}

func envExample(name, dbType string) string {
	dbURL := "postgres://user:pass@localhost:5432/" + name + "?sslmode=disable"
	if dbType == "mongo" {
		dbURL = "mongodb://localhost:27017/" + name
	} else if dbType == "mysql" {
		dbURL = "user:pass@tcp(localhost:3306)/" + name
	}
	return fmt.Sprintf(`DATABASE_URL=%s
REDIS_URL=redis://localhost:6379
JWT_SECRET=change-me-to-a-random-string
API_URL=http://localhost:8080
PORT=8080
`, dbURL)
}

func gitignore(name, dbType string) string {
	return `# Binaries
*.exe
*.dll
*.so
*.dylib
/app
/fileserver

# Go
/vendor/

# Node (web build only)
node_modules/
dist/
.vite/

# IDE
.idea/
.vscode/
*.swp
*~

# OS
.DS_Store
Thumbs.db

# Environment
.env
.env.local
.env.*.local

# Build
/build/
/tmp/
coverage.out
`
}

func apiMainGo(name, dbType string) string {
	return fmt.Sprintf(`package main

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

	// Graceful shutdown
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
}

func apiRoutesGo(name, dbType string) string {
	return `package main

import "net/http"

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /api/hello", handleHello)
}
`
}

func apiHealthGo(name, dbType string) string {
	return `package main

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": "1.0.0",
		"uptime":  time.Since(startTime).String(),
	})
}
`
}

func apiHelloGo(name, dbType string) string {
	return fmt.Sprintf(`package main

import (
	"encoding/json"
	"net/http"
)

func handleHello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Hello from %s",
	})
}
`, name)
}

func apiDockerfile(name, dbType string) string {
	return `FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app .

FROM scratch
COPY --from=builder /build/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/app"]
`
}

func apiQuikdbJson(name, dbType string) string {
	return fmt.Sprintf(`{
  "name": "%s-api",
  "type": "api",
  "buildCommand": "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o app .",
  "startCommand": "./app",
  "envVars": {}
}
`, name)
}

func apiGoMod(name, dbType string) string {
	return fmt.Sprintf(`module %s/services/api

go 1.23
`, name)
}

func webServerGo(name, dbType string) string {
	return `package main

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
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		staticDir = "./dist"
	}
	fs := http.FileServer(http.Dir(staticDir))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(` + "`" + `{"status":"ok"}` + "`" + `))
			return
		}
		if strings.Contains(r.URL.Path, ".") {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, staticDir+"/index.html")
	})

	log.Printf("web service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}
`
}

func webGoMod(name, dbType string) string {
	return fmt.Sprintf(`module %s/services/web

go 1.23
`, name)
}

func webIndexHtml(name, dbType string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>%s</title>
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/index.tsx"></script>
</body>
</html>
`, name)
}

func webPackageJson(name, dbType string) string {
	return fmt.Sprintf(`{
  "name": "%s-web",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "preact": "^10.25.0"
  },
  "devDependencies": {
    "@preact/preset-vite": "^2.9.0",
    "vite": "^6.0.0"
  }
}
`, name)
}

func webViteConfig(name, dbType string) string {
	return `import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';

export default defineConfig({
  plugins: [preact()],
  build: {
    outDir: 'dist',
  },
});
`
}

func webIndexTsx(name, dbType string) string {
	return `import { render } from 'preact';
import { App } from './app';

render(<App />, document.getElementById('app')!);
`
}

func webAppTsx(name, dbType string) string {
	return fmt.Sprintf(`import { useState, useEffect } from 'preact/hooks';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export function App() {
  const [message, setMessage] = useState('Loading...');

  useEffect(() => {
    fetch(API_URL + '/api/hello')
      .then(r => r.json())
      .then(data => setMessage(data.message))
      .catch(() => setMessage('Could not connect to API'));
  }, []);

  return (
    <div style={{ maxWidth: '600px', margin: '40px auto', fontFamily: 'system-ui, sans-serif', padding: '0 20px' }}>
      <h1 style={{ fontSize: '24px', marginBottom: '8px' }}>%s</h1>
      <p style={{ color: '#666' }}>{message}</p>
      <p style={{ marginTop: '32px', fontSize: '13px', color: '#999' }}>
        Built with quikdb-frame
      </p>
    </div>
  );
}
`, name)
}

func webDockerfile(name, dbType string) string {
	return `FROM node:22-alpine AS builder
WORKDIR /build
COPY package.json ./
RUN npm install
COPY . .
RUN npm run build

FROM golang:1.23-alpine AS server
WORKDIR /build
COPY server.go go.mod ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o fileserver .

FROM scratch
COPY --from=server /build/fileserver /fileserver
COPY --from=builder /build/dist /static
EXPOSE 3000
ENTRYPOINT ["/fileserver"]
`
}

func webQuikdbJson(name, dbType string) string {
	return fmt.Sprintf(`{
  "name": "%s-web",
  "type": "web",
  "buildCommand": "npm run build && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o fileserver server.go",
  "startCommand": "./fileserver",
  "envVars": {}
}
`, name)
}

func sharedDbGo(name, dbType string) string {
	return `package db

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Database interface — all adapters implement this.
type Database interface {
	Connect(ctx context.Context) error
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
	Health() string
}

// ConnectWithRetry connects to the database with exponential backoff.
func ConnectWithRetry(db Database, maxRetries int) error {
	ctx := context.Background()
	delays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond}

	for i := 0; i <= maxRetries; i++ {
		if err := db.Connect(ctx); err != nil {
			if i == maxRetries {
				return fmt.Errorf("failed to connect after %d retries: %w", maxRetries, err)
			}
			delay := delays[i]
			if i >= len(delays) {
				delay = delays[len(delays)-1]
			}
			time.Sleep(delay)
			continue
		}
		return nil
	}
	return nil
}

// GetDatabaseURL reads DATABASE_URL from environment.
func GetDatabaseURL() string {
	return os.Getenv("DATABASE_URL")
}
`
}

func sharedJwtGo(name, dbType string) string {
	return `package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Claims struct {
	UserID string ` + "`json:\"userId\"`" + `
	Email  string ` + "`json:\"email\"`" + `
	Role   string ` + "`json:\"role\"`" + `
	Iat    int64  ` + "`json:\"iat\"`" + `
	Exp    int64  ` + "`json:\"exp\"`" + `
}

// CreateToken creates a signed JWT with the given claims.
func CreateToken(userID, email, role string, expiry time.Duration) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", fmt.Errorf("JWT_SECRET not set")
	}

	now := time.Now().Unix()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		Iat:    now,
		Exp:    now + int64(expiry.Seconds()),
	}

	header := base64URLEncode([]byte(` + "`" + `{"alg":"HS256","typ":"JWT"}` + "`" + `))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64URLEncode(payload)

	sigInput := header + "." + payloadB64
	sig := signHS256(sigInput, secret)

	return sigInput + "." + sig, nil
}

// VerifyToken verifies a JWT and returns the claims.
// Tries JWT_SECRET first, falls back to JWT_SECRET_OLD for rotation.
func VerifyToken(tokenStr string) (*Claims, error) {
	secret := os.Getenv("JWT_SECRET")
	secretOld := os.Getenv("JWT_SECRET_OLD")

	claims, err := verifyWithSecret(tokenStr, secret)
	if err != nil && secretOld != "" {
		claims, err = verifyWithSecret(tokenStr, secretOld)
	}
	if err != nil {
		return nil, err
	}

	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return claims, nil
}

func verifyWithSecret(tokenStr, secret string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	sigInput := parts[0] + "." + parts[1]
	expectedSig := signHS256(sigInput, secret)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid signature")
	}

	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}

	return &claims, nil
}

func signHS256(input, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(input))
	return base64URLEncode(h.Sum(nil))
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func base64URLDecode(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
`
}

func sharedMiddlewareGo(name, dbType string) string {
	return `package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const UserIDKey contextKey = "userId"
const UserRoleKey contextKey = "userRole"

// AuthMiddleware validates JWT tokens on incoming requests.
// It does NOT query the database — only verifies signature and expiry.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, ` + "`" + `{"error":"unauthorized"}` + "`" + `, http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := VerifyToken(token)
		if err != nil {
			http.Error(w, ` + "`" + `{"error":"invalid token"}` + "`" + `, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		ctx = context.WithValue(ctx, UserRoleKey, claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserID extracts the user ID from the request context.
func GetUserID(r *http.Request) string {
	if v, ok := r.Context().Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}
`
}

func sharedLoggerGo(name, dbType string) string {
	return `package logging

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"crypto/rand"
	"encoding/hex"
)

var logger = log.New(os.Stdout, "", 0)

type logEntry struct {
	Level     string ` + "`json:\"level\"`" + `
	Msg       string ` + "`json:\"msg\"`" + `
	RequestID string ` + "`json:\"requestId,omitempty\"`" + `
	Method    string ` + "`json:\"method,omitempty\"`" + `
	Path      string ` + "`json:\"path,omitempty\"`" + `
	Status    int    ` + "`json:\"status,omitempty\"`" + `
	Duration  int64  ` + "`json:\"duration_ms,omitempty\"`" + `
	Timestamp string ` + "`json:\"ts\"`" + `
}

// Info logs an info-level JSON message.
func Info(msg string) {
	entry := logEntry{Level: "info", Msg: msg, Timestamp: time.Now().UTC().Format(time.RFC3339)}
	data, _ := json.Marshal(entry)
	logger.Println(string(data))
}

// Error logs an error-level JSON message.
func Error(msg string) {
	entry := logEntry{Level: "error", Msg: msg, Timestamp: time.Now().UTC().Format(time.RFC3339)}
	data, _ := json.Marshal(entry)
	logger.Println(string(data))
}

// RequestLogger middleware logs every HTTP request as structured JSON.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = generateRequestID()
		}
		w.Header().Set("X-Request-Id", requestID)

		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)

		entry := logEntry{
			Level:     "info",
			Msg:       "request completed",
			RequestID: requestID,
			Method:    r.Method,
			Path:      r.URL.Path,
			Status:    sw.status,
			Duration:  time.Since(start).Milliseconds(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		data, _ := json.Marshal(entry)
		logger.Println(string(data))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "req-" + hex.EncodeToString(b)
}
`
}

func sharedUserType(name, dbType string) string {
	return `package types

import "time"

type User struct {
	ID        string    ` + "`json:\"id\"`" + `
	Email     string    ` + "`json:\"email\"`" + `
	Name      string    ` + "`json:\"name\"`" + `
	Role      string    ` + "`json:\"role\"`" + `
	CreatedAt time.Time ` + "`json:\"createdAt\"`" + `
	UpdatedAt time.Time ` + "`json:\"updatedAt\"`" + `
}
`
}

func configDatabase(name, dbType string) string {
	return fmt.Sprintf(`database:
  adapter: %s
  url: ${DATABASE_URL}
  pool:
    maxConns: 20
    minConns: 2
    maxConnLifetime: 1h
    maxConnIdleTime: 30m
    healthCheckPeriod: 1m
`, dbType)
}

func configRatelimit(name, dbType string) string {
	return `ratelimit:
  rules:
    - match: /api/auth/*
      key: ip
      max: 10
      window: 1m
    - match: /api/*
      key: user
      max: 100
      window: 1m
    - match: /webhooks/*
      key: ip
      max: 1000
      window: 1m
`
}

func claudeMd(name, dbType string) string {
	return fmt.Sprintf(`# %s — quikdb-frame project

## Architecture
This is a multi-service project built with quikdb-frame.
Each service is a Go binary in services/. Shared code lives in shared/.

## Service rules
- api services: REST endpoints + business logic. One domain per service.
- web service: Preact frontend. Pure client-side. No SSR.
- ws services: WebSocket. Gateway routes events, workers process them.
- worker services: Background jobs. Queue consumers or cron.

## Strict rules
- All Go services: CGO_ENABLED=0, single static binary, scratch Docker image
- All services read PORT from environment
- All HTTP services have GET /health returning JSON with status
- NO node_modules in production images. Node is build-time only for web.
- NO interpreted runtimes in production containers
- Database access through shared/db/ only
- Auth tokens validated by shared/auth/middleware.go
- Payment webhooks must verify signatures before processing
- All financial operations must be idempotent
- Graceful shutdown on SIGTERM in every service
`, name)
}

func cursorrules(name, dbType string) string {
	return claudeMd(name, dbType)
}

func copilotInstructions(name, dbType string) string {
	return claudeMd(name, dbType)
}
