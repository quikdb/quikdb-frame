# Hello World — quikdb-frame example

A minimal quikdb-frame project with one API service and one web service.

## Structure

```
hello-world/
  quikdb.yaml              # Project manifest
  services/
    api/                    # Go API service (3 endpoints)
    web/                    # Preact frontend with Go file server
```

## Run locally

```bash
# API service
cd services/api
go run .
# Listening on :8080

# Web service (in another terminal)
cd services/web
npm install && npm run dev
# Listening on :3000
```

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | /health | Health check |
| GET | /api/hello | Hello message |
| GET | /api/tasks | List tasks |

## Build

```bash
# API binary
cd services/api
CGO_ENABLED=0 go build -ldflags="-s -w" -o app .
# Result: ~5MB binary

# Web (build frontend, then file server)
cd services/web
npm run build
CGO_ENABLED=0 go build -ldflags="-s -w" -o fileserver server.go
```

## Deploy to QuikDB

```bash
quikdb-frame deploy
```
