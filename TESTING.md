# quikdb-frame Testing Flow

Complete testing guide to verify speed, size, and efficiency of quikdb-frame.

## Prerequisites

- Go 1.23+ installed (`go version`)
- Node.js 18+ installed (`node --version`)
- Git installed
- macOS, Linux, or WSL

## 1. Install quikdb-frame

```bash
# Clone and build
git clone https://github.com/quikdb/quikdb-frame.git
cd quikdb-frame
go build -ldflags="-s -w" -o quikdb-frame ./cmd/quikdb-frame

# Verify it runs
./quikdb-frame version
# Expected: quikdb-frame v0.1.0

# Optional: move to PATH
sudo mv quikdb-frame /usr/local/bin/
```

### Test 1A: CLI Binary Size

```bash
ls -lh quikdb-frame
# Expected: ~2-4 MB (stripped)
# PASS if under 5 MB
```

### Test 1B: CLI Startup Time

```bash
time quikdb-frame version
# Expected: < 50ms
# PASS if real time is under 100ms
```

Record:
- [ ] Binary size: ______ MB
- [ ] Startup time: ______ ms

---

## 2. Project Scaffolding

### Test 2A: Init Speed

```bash
cd /tmp
time quikdb-frame init test-app
# Expected: < 500ms
# PASS if under 1 second
```

Record:
- [ ] Init time: ______ ms
- [ ] All files created (see checklist below)

### Test 2B: File Structure Verification

```bash
cd test-app
```

Verify these exist:

- [ ] `quikdb.yaml`
- [ ] `.env.example`
- [ ] `.gitignore`
- [ ] `CLAUDE.md`
- [ ] `.cursorrules`
- [ ] `.github/copilot-instructions.md`
- [ ] `config/database.yaml`
- [ ] `config/ratelimit.yaml`
- [ ] `shared/db/database.go`
- [ ] `shared/auth/jwt.go`
- [ ] `shared/auth/middleware.go`
- [ ] `shared/logging/logger.go`
- [ ] `shared/types/user.go`
- [ ] `services/api/main.go`
- [ ] `services/api/routes.go`
- [ ] `services/api/health.go`
- [ ] `services/api/hello.go`
- [ ] `services/api/Dockerfile`
- [ ] `services/api/quikdb.json`
- [ ] `services/api/go.mod`
- [ ] `services/web/server.go`
- [ ] `services/web/go.mod`
- [ ] `services/web/index.html`
- [ ] `services/web/package.json`
- [ ] `services/web/vite.config.ts`
- [ ] `services/web/src/index.tsx`
- [ ] `services/web/src/app.tsx`
- [ ] `services/web/Dockerfile`
- [ ] `services/web/quikdb.json`

### Test 2C: Init with Different Databases

```bash
cd /tmp
quikdb-frame init test-mongo --db mongo
grep "type: mongo" test-mongo/quikdb.yaml
# PASS if mongo appears in database config

quikdb-frame init test-mysql --db mysql
grep "type: mysql" test-mysql/quikdb.yaml
# PASS if mysql appears

# Cleanup
rm -rf test-mongo test-mysql
```

---

## 3. API Service Build & Run

### Test 3A: API Build Speed

```bash
cd /tmp/test-app/services/api
time go build -ldflags="-s -w" -o api-server .
# Expected: < 5 seconds (first build, includes downloading deps)
# Subsequent builds: < 1 second
```

### Test 3B: API Binary Size

```bash
ls -lh api-server
# Expected: ~5-7 MB (stripped)
# PASS if under 10 MB
```

### Test 3C: API Cold Start

```bash
time (./api-server &; sleep 0.2; curl -s http://localhost:8080/health > /dev/null; kill %1 2>/dev/null)
# Expected: Server responds within 200ms of starting
```

Alternative manual test:

```bash
./api-server &
API_PID=$!

# Test health endpoint
curl -s http://localhost:8080/health | python3 -m json.tool
# Expected: {"status": "ok", "uptime": "..."}

# Test hello endpoint
curl -s http://localhost:8080/api/hello | python3 -m json.tool
# Expected: {"message": "Hello from test-app", "service": "api"}

# Check memory usage
ps -o rss= -p $API_PID | awk '{print $1/1024 " MB"}'
# Expected: < 10 MB idle
# PASS if under 15 MB

kill $API_PID
```

Record:
- [ ] Build time (first): ______ s
- [ ] Build time (second): ______ s
- [ ] Binary size: ______ MB
- [ ] Health endpoint works: yes/no
- [ ] Hello endpoint works: yes/no
- [ ] Idle memory: ______ MB

---

## 4. Docker Image

### Test 4A: Docker Build

```bash
cd /tmp/test-app/services/api
docker build --platform linux/amd64 -t test-api .
# PASS if build succeeds
```

### Test 4B: Docker Image Size

```bash
docker images test-api --format "{{.Size}}"
# Expected: < 15 MB (scratch base + static binary)
# PASS if under 20 MB
```

### Test 4C: Docker Run

```bash
docker run -d --name test-api-container -p 9090:8080 test-api
curl -s http://localhost:9090/health | python3 -m json.tool
# Expected: {"status": "ok", ...}

docker stats test-api-container --no-stream --format "{{.MemUsage}}"
# Expected: < 10 MiB

docker rm -f test-api-container
```

Record:
- [ ] Docker image size: ______ MB
- [ ] Container starts: yes/no
- [ ] Health responds in container: yes/no
- [ ] Container memory: ______ MiB

---

## 5. Add Service Command

### Test 5A: Add API Service

```bash
cd /tmp/test-app
quikdb-frame add api auth
```

Verify:
- [ ] `services/auth/main.go` exists
- [ ] `services/auth/routes.go` exists
- [ ] `services/auth/Dockerfile` exists
- [ ] `services/auth/quikdb.json` exists
- [ ] `services/auth/go.mod` exists

```bash
cd services/auth && go build -o /dev/null . && echo "BUILD OK"
# PASS if "BUILD OK" prints
cd ../..
```

### Test 5B: Add WebSocket Service

```bash
quikdb-frame add ws realtime
```

Verify:
- [ ] `services/realtime/main.go` exists
- [ ] `services/realtime/Dockerfile` exists
- [ ] WebSocket upgrade handling in main.go

```bash
cd services/realtime && go build -o /dev/null . && echo "BUILD OK"
cd ../..
```

### Test 5C: Add Worker Service

```bash
quikdb-frame add worker jobs
```

Verify:
- [ ] `services/jobs/main.go` exists
- [ ] Worker loop pattern in main.go

```bash
cd services/jobs && go build -o /dev/null . && echo "BUILD OK"
cd ../..
```

### Test 5D: Add Web Service

```bash
quikdb-frame add web dashboard
```

Verify:
- [ ] `services/dashboard/server.go` exists
- [ ] `services/dashboard/package.json` exists
- [ ] `services/dashboard/index.html` exists

---

## 6. Dev Mode

### Test 6A: Run All Services

```bash
cd /tmp/test-app
quikdb-frame dev
# Expected: All services start with prefixed output
# Each service gets a unique port (8080, 8081, 8082, ...)
# Ctrl+C should gracefully stop all services
```

Verify:
- [ ] API service starts and responds
- [ ] Prefixed output shows service names
- [ ] Ctrl+C stops all services cleanly

### Test 6B: Run Single Service

```bash
quikdb-frame dev api
# Expected: Only the api service starts
```

---

## 7. Express Converter

### Test 7A: Create a Test Express App

```bash
mkdir -p /tmp/express-test/routes
cat > /tmp/express-test/app.js << 'JSEOF'
const express = require('express');
const app = express();

app.use(express.json());

app.get('/health', (req, res) => res.json({ status: 'ok' }));
app.get('/api/users', (req, res) => res.json([]));
app.post('/api/users', (req, res) => res.json({ created: true }));
app.get('/api/users/:id', (req, res) => res.json({ id: req.params.id }));
app.put('/api/users/:id', (req, res) => res.json({ updated: true }));
app.delete('/api/users/:id', (req, res) => res.json({ deleted: true }));
app.get('/api/posts', (req, res) => res.json([]));
app.post('/api/posts', (req, res) => res.json({ created: true }));

app.listen(3000);
JSEOF

cat > /tmp/express-test/package.json << 'PKGEOF'
{
  "name": "express-test",
  "dependencies": {
    "express": "^4.18.0",
    "mongoose": "^7.0.0"
  }
}
PKGEOF

cat > /tmp/express-test/.env.example << 'ENVEOF'
DATABASE_URL=mongodb://localhost:27017/test
JWT_SECRET=secret
PORT=3000
ENVEOF
```

### Test 7B: Run Converter

```bash
time quikdb-frame convert /tmp/express-test --from express
# Expected: Scan summary, generated output
```

Verify:
- [ ] Output directory `/tmp/express-test-quikdb/` created
- [ ] `quikdb.yaml` exists with `type: mongo` (detected from mongoose)
- [ ] `services/api/routes.go` has route registrations
- [ ] `services/api/handlers.go` has handler stubs
- [ ] `services/api/main.go` compiles
- [ ] `.env.example` contains DATABASE_URL, JWT_SECRET, PORT
- [ ] Health route is NOT duplicated

```bash
cd /tmp/express-test-quikdb/services/api
go build -o /dev/null . && echo "CONVERTER OUTPUT COMPILES"
# PASS if it compiles
```

Record:
- [ ] Routes detected: ______ (expected: 7-8)
- [ ] DB type detected: ______ (expected: mongo)
- [ ] Env vars detected: ______ (expected: 3)
- [ ] Output compiles: yes/no

---

## 8. Load Testing (Optional)

### Test 8A: Requests Per Second

```bash
cd /tmp/test-app/services/api
go build -ldflags="-s -w" -o api-server .
./api-server &
API_PID=$!

# Using hey (install: go install github.com/rakyll/hey@latest)
hey -n 10000 -c 100 http://localhost:8080/health

# Expected: > 20,000 req/s on modern hardware
# PASS if > 10,000 req/s

kill $API_PID
```

### Test 8B: Memory Under Load

```bash
./api-server &
API_PID=$!

# Before load
ps -o rss= -p $API_PID | awk '{print "Before: " $1/1024 " MB"}'

# Run load
hey -n 50000 -c 200 http://localhost:8080/health

# After load
ps -o rss= -p $API_PID | awk '{print "After: " $1/1024 " MB"}'
# Expected: < 30 MB even after 50k requests
# PASS if under 50 MB

kill $API_PID
```

Record:
- [ ] Requests/sec: ______
- [ ] P99 latency: ______ ms
- [ ] Memory before load: ______ MB
- [ ] Memory after load: ______ MB

---

## 9. Comparison Benchmarks

Run these to compare against other frameworks:

### Test 9A: Binary Size Comparison

| Framework | Binary/Bundle Size |
|-----------|-------------------|
| quikdb-frame API | ______ MB |
| Express (node_modules) | ~50-100 MB |
| NestJS (node_modules) | ~150-300 MB |
| FastAPI (venv) | ~50-100 MB |
| Spring Boot (jar) | ~30-50 MB |

### Test 9B: Docker Image Comparison

| Framework | Docker Image |
|-----------|-------------|
| quikdb-frame API | ______ MB |
| Express (node:alpine) | ~150-200 MB |
| NestJS (node:alpine) | ~200-400 MB |
| FastAPI (python:slim) | ~150-200 MB |
| Spring Boot | ~300-500 MB |

### Test 9C: Cold Start Comparison

| Framework | Cold Start |
|-----------|-----------|
| quikdb-frame API | ______ ms |
| Express | ~200-500 ms |
| NestJS | ~500-2000 ms |
| FastAPI | ~300-800 ms |
| Spring Boot | ~2000-5000 ms |

### Test 9D: Idle Memory Comparison

| Framework | Idle Memory |
|-----------|------------|
| quikdb-frame API | ______ MB |
| Express | ~30-50 MB |
| NestJS | ~80-150 MB |
| FastAPI | ~30-50 MB |
| Spring Boot | ~150-300 MB |

---

## 10. Auth & Deploy (Requires QuikDB Account)

### Test 10A: Login

```bash
quikdb-frame login
# Expected: Opens browser for QuikDB Compute auth
# After auth: "Logged in successfully."

# Verify token saved
ls -la ~/.quikdb-frame/auth.json
# PASS if file exists with 0600 permissions
```

### Test 10B: Token Login

```bash
quikdb-frame logout
quikdb-frame login --token YOUR_API_TOKEN
# Expected: "Token saved. You are now logged in."
```

### Test 10C: Status

```bash
quikdb-frame status
# Expected: List of deployments or "No deployments found."
```

### Test 10D: Deploy (Requires GitHub Repo)

```bash
cd /tmp/test-app
git init && git add . && git commit -m "init"
# Create a GitHub repo and push
# gh repo create test-quikdb-frame --public --push

quikdb-frame deploy
# Expected: Deploys to QuikDB Compute
# Shows deployment ID, status, and URL
```

---

## Summary Scorecard

Fill in after completing all tests:

| Metric | Target | Actual | Pass? |
|--------|--------|--------|-------|
| CLI binary size | < 5 MB | | |
| CLI startup | < 100ms | | |
| Init time | < 1s | | |
| API binary size | < 10 MB | | |
| API cold start | < 200ms | | |
| API idle memory | < 15 MB | | |
| Docker image | < 20 MB | | |
| Container memory | < 10 MiB | | |
| All services build | 100% | | |
| Converter compiles | yes | | |
| Requests/sec | > 10k | | |
| P99 latency | < 5ms | | |

### Grading

- 12/12 pass: Ship it
- 10-11/12 pass: Ship with notes
- 8-9/12 pass: Fix before shipping
- < 8/12 pass: Major issues, block release

---

## Cleanup

```bash
rm -rf /tmp/test-app /tmp/test-mongo /tmp/test-mysql /tmp/express-test /tmp/express-test-quikdb
docker rmi test-api 2>/dev/null
```
