# quikdb-frame Specification

Version: 0.2.0

quikdb-frame is the operating system for QuikDB applications. It defines how apps are structured, built, deployed, scaled, and observed on QuikDB Compute. It covers project structure, CLI, deploy pipeline, auth, payments, observability, and developer workflow — designed to work with any AI coding assistant.

---

## Principles

1. **Compiled over interpreted.** All backend services compile to static Go binaries. No runtime, no interpreter, no package manager in production. A single binary IS the app.
2. **Zero dependency bleed.** No `node_modules`, no `pip install`, no `vendor/` in production images. Every dependency is compiled into the binary at build time. If a dependency cannot be statically linked, it is not allowed. This pattern is strictly forbidden: shipping a package manager or dependency tree into a production container.
3. **Start simple, split when ready.** A new project starts with one `api` and one `web` service. As the app grows, developers split into multiple services using `quikdb-frame add`. The framework supports multi-service from day one, but does not force it on day one.
4. **Stateless services.** No local file persistence. State lives in the database. Containers can restart anytime.
5. **AI-assisted, not AI-dependent.** The spec and templates are clear enough for any AI tool to follow. quikdb-frame does not ship its own AI.
6. **Deploy-ready from scaffold.** A freshly scaffolded project deploys to QuikDB without editing a single file.
7. **Production-first.** Every scaffold includes auth, health checks, graceful shutdown, structured logging, rate limiting, and database connection pooling. Not added later. Present from the first commit.

---

## Service Types

Every quikdb-frame project is composed of one or more service types. A new project starts with `api` + `web`. As the app grows, developers add services using `quikdb-frame add`. Projects can have **multiple services of the same type** — for example, `api-auth`, `api-users`, `api-payments` are three separate `api` services. But splitting is a choice, not a starting point.

### api

REST API and business logic. Each api service owns one domain.

| Property | Constraint |
|---|---|
| Language | Go |
| HTTP | Standard library `net/http` or `echo` |
| Image target | < 15MB (scratch or distroless) |
| RAM target | < 10MB idle |
| Cold start target | < 200ms |
| Binary | Single static binary, `CGO_ENABLED=0` |
| Health check | `GET /health` returns `200 {"status":"ok","db":"connected"}` |
| PORT | Reads from `PORT` env var, default `8080` |

Example domain split:

| Service | Owns |
|---|---|
| `api-auth` | Registration, login, OTP, OAuth, JWT, sessions, token refresh |
| `api-users` | Profiles, settings, avatars, preferences |
| `api-products` | Catalog, inventory, categories, search |
| `api-payments` | Checkout, webhooks, ledger, refunds, subscriptions |
| `api-orders` | Order lifecycle, fulfillment, tracking |
| `api-media` | Upload URLs, metadata, processing status |
| `api-admin` | Admin-only operations, moderation, analytics |

Each api service is its own binary, its own container, its own deployment. They share types and database connection config from `shared/`.

### web

Frontend UI. Pure client-side with a tiny static file server.

| Property | Constraint |
|---|---|
| UI framework | **Preact** (3KB) + Preact Signals for state |
| Bundler | Vite |
| Server | Go static file server (< 5MB binary) |
| Image target | < 20MB |
| RAM target | < 10MB idle |
| Build output | Static files in `dist/` |
| PORT | Reads from `PORT` env var, default `3000` |
| Health check | `GET /` returns `200` |
| API calls | All calls go to api services via `API_URL` env var |

**Why Preact, not React:** React is 42KB minified+gzipped. Preact is 3KB. Same JSX API, same component model, same hooks, compatible with most React libraries via `preact/compat`. For a framework built on the principle of zero waste, React is a contradiction.

**Component library:** quikdb-frame ships a minimal component library built on Preact + CSS modules. No Tailwind (adds 30KB+ purged CSS), no CSS-in-JS runtime. Components:

- Layout (Container, Stack, Grid, Sidebar)
- Typography (Text, Heading, Link)
- Forms (Input, Select, Checkbox, Radio, Switch, TextArea, FileUpload)
- Feedback (Alert, Toast, Modal, Drawer, Skeleton, Spinner)
- Navigation (Navbar, Tabs, Breadcrumb, Pagination)
- Data (Table, Card, Badge, Avatar, List)

Each component is a single `.tsx` file with a co-located `.module.css` file. Import only what you use. No tree-shaking needed because there is no bundle — each component is a direct import.

**SEO:** The Go static file server detects bot user agents (`Googlebot`, `Twitterbot`, `facebookexternalhit`, `LinkedInBot`) and serves pre-rendered HTML with Open Graph and Twitter Card meta tags. For human users, the SPA loads normally. Pre-rendering runs at build time via `preact-render-to-string`.

**Routing:** Client-side routing via `preact-router`. The Go file server returns `index.html` for all non-file paths (SPA fallback).

### mobile

Native mobile application.

| Property | Constraint |
|---|---|
| Framework | **Flutter** (default) or **Expo** (React Native) |
| Architecture | Feature packages in a monorepo |
| API calls | HTTP to api services |
| Real-time | WebSocket to ws services |
| Local storage | SQLite (Drift for Flutter, expo-sqlite for Expo) |
| Auth | JWT stored in secure storage (FlutterSecureStorage / expo-secure-store) |

**Why Flutter as default:** Compiles to native on iOS and Android from a single codebase. No JavaScript bridge, no runtime overhead.

**Why Expo as alternative:** For teams already in JavaScript. Expo manages native builds without touching Xcode/Android Studio. Choose at `quikdb-frame init`.

**Package structure (Flutter):**

```
mobile/
  packages/
    core/           <- App shell, navigation, DI
    auth/           <- Login, registration, OTP, OAuth
    feed/           <- Main content feed
    chat/           <- Messaging
    profile/        <- User profile
    settings/       <- App settings
    shared/         <- Common widgets, themes, API client
```

**Package structure (Expo):**

```
mobile/
  apps/
    main/           <- App entry, navigation
  packages/
    auth/
    feed/
    chat/
    ui/             <- Shared components
    api-client/     <- Generated API client
```

The mobile service is not deployed on QuikDB. It connects to deployed api and ws services.

### ws

WebSocket server. Handles all real-time connections.

| Property | Constraint |
|---|---|
| Language | Go |
| WebSocket | `nhooyr.io/websocket` (standard library compatible) |
| Image target | < 10MB |
| RAM target | < 10MB idle + ~2KB per connection |
| Binary | Single static binary |
| PORT | Reads from `PORT` env var, default `8081` |
| Health check | `GET /health` returns `200` |
| Upgrade endpoint | `GET /ws` upgrades to WebSocket |

Like api services, ws services can be split by domain:

| Service | Handles |
|---|---|
| `ws-chat` | Direct messages, group chats, typing indicators |
| `ws-notifications` | Push events, live updates, alerts |
| `ws-live` | Live streaming events, comments, reactions |
| `ws-feed` | Real-time feed updates |

**Architecture pattern:**

A single **gateway** service accepts all WebSocket connections, authenticates them, and routes events to the correct ws worker via Redis Streams. Workers consume events, execute business logic, and push responses back through the gateway.

```
Client → ws-gateway (connection management only, zero business logic)
  → Redis Streams (event routing by type)
    → ws-chat (consumes stream:chat)
    → ws-notifications (consumes stream:notifications)
    → ws-live (consumes stream:live)
```

The gateway is stateless except for active connections. Workers are independently scalable.

**Connection auth:** JWT validated on WebSocket upgrade. Token passed in `Authorization` header or first message. Gateway does NOT query the database — it only verifies the JWT signature.

### worker

Background jobs. Runs on a schedule, from a queue, or event-driven.

| Property | Constraint |
|---|---|
| Language | Go |
| Image target | < 10MB |
| RAM target | < 10MB during execution |
| Binary | Single static binary |
| Trigger | Cron schedule, Redis Stream consumer, or HTTP trigger |
| Health check | Not required for cron workers. Required for queue consumers (long-running). |

Workers split by function:

| Service | Trigger | Job |
|---|---|---|
| `worker-email` | Queue (Redis Stream) | Transactional and marketing emails |
| `worker-media` | Queue (Redis Stream) | Image resize, video transcode, thumbnail generation |
| `worker-cleanup` | Cron (daily) | Delete expired sessions, orphaned data, temp files |
| `worker-digest` | Cron (weekly) | Weekly summary emails, analytics reports |
| `worker-sync` | Event (database change) | Sync data to search index, cache invalidation |
| `worker-payments` | Queue (Redis Stream) | Process webhook events, retry failed charges |

**Queue pattern:**

Workers consume from Redis Streams using consumer groups. This gives:
- At-least-once delivery
- Consumer group load balancing
- Dead letter handling (messages pending beyond timeout are requeued)
- No external queue service needed (Redis already present for caching)

```go
// Worker reads from a Redis Stream
for {
    messages := redis.XReadGroup("worker-email-group", "consumer-1", "stream:emails", ">", 10)
    for _, msg := range messages {
        process(msg)
        redis.XAck("stream:emails", "worker-email-group", msg.ID)
    }
}
```

---

## Project Structure

A production-ready e-commerce project:

```
my-app/
  quikdb.yaml                     <- Project manifest
  .env                            <- Environment variables (gitignored)
  .env.example                    <- Template (committed)

  shared/
    db/
      postgres.go                 <- PostgreSQL connection pool + health check
      mongo.go                    <- MongoDB connection (if using Mongo)
      redis.go                    <- Redis connection + helpers
      migrations/
        001_create_users.up.sql
        001_create_users.down.sql
        002_create_products.up.sql
    auth/
      jwt.go                      <- JWT create, verify, refresh, claims
      oauth.go                    <- Google, Apple, GitHub OAuth verification
      otp.go                      <- OTP generation, verification, rate limiting
      password.go                 <- Argon2id hashing, upgrade from legacy
      middleware.go               <- Auth middleware for api services
      apikey.go                   <- API key generation, validation
    types/
      user.go
      product.go
      order.go
      payment.go
    queue/
      producer.go                 <- Redis Stream XADD helper
      consumer.go                 <- Redis Stream XREADGROUP consumer base
      streams.go                  <- Stream name constants
    cache/
      cache.go                    <- Cache-aside pattern with TTL
    notify/
      push.go                     <- FCM + APNs push notifications
      email.go                    <- SMTP / SendGrid / SES
      sms.go                      <- Twilio / Vonage with fallback chain
    logging/
      logger.go                   <- Structured JSON logger
      request_id.go               <- Request ID middleware + propagation
    payments/
      stripe.go                   <- Stripe client, checkout, webhooks
      flutterwave.go              <- Flutterwave client, payments
      paypal.go                   <- PayPal client
      paystack.go                 <- Paystack client
      lemonsqueezy.go             <- Lemon Squeezy client
      razorpay.go                 <- Razorpay client
      revenuecat.go               <- RevenueCat (mobile IAP)
      router.go                   <- Currency-to-provider routing
      ledger.go                   <- Transaction ledger with balance snapshots
      webhook.go                  <- Signature verification per provider
      idempotency.go              <- Idempotency key checking
    storage/
      s3.go                       <- S3 upload, presigned URLs
      local.go                    <- Local file storage (dev only)

  services/
    api-auth/
      main.go
      routes.go
      handlers/
        register.go
        login.go
        otp.go
        oauth.go
        refresh.go
        logout.go
      Dockerfile
      quikdb.json
      go.mod

    api-users/
      main.go
      routes.go
      handlers/
        profile.go
        settings.go
        avatar.go
      Dockerfile
      quikdb.json
      go.mod

    api-products/
      main.go
      routes.go
      handlers/
        catalog.go
        search.go
        categories.go
      Dockerfile
      quikdb.json
      go.mod

    api-payments/
      main.go
      routes.go
      handlers/
        checkout.go
        webhooks.go
        subscriptions.go
        refunds.go
      Dockerfile
      quikdb.json
      go.mod

    web/
      src/
        index.tsx
        app.tsx
        pages/
        components/
        hooks/
        lib/
          api.ts                  <- API client, reads API_URL from env
          ws.ts                   <- WebSocket client
      index.html
      vite.config.ts
      package.json
      Dockerfile
      quikdb.json

    ws-gateway/
      main.go
      auth.go
      router.go                   <- Event → Redis Stream mapping
      throttle.go                 <- Per-connection event throttling
      Dockerfile
      quikdb.json
      go.mod

    ws-chat/
      main.go
      handlers/
        message.go
        room.go
        typing.go
      Dockerfile
      quikdb.json
      go.mod

    ws-notifications/
      main.go
      handlers/
        push.go
        live_update.go
      Dockerfile
      quikdb.json
      go.mod

    worker-email/
      main.go
      templates/
        welcome.html
        reset_password.html
        order_confirmation.html
      Dockerfile
      quikdb.json
      go.mod

    worker-media/
      main.go
      processors/
        image.go
        thumbnail.go
      Dockerfile
      quikdb.json
      go.mod

    mobile/
      ... (Flutter or Expo structure)

  CLAUDE.md                       <- AI instructions for Claude Code
  .cursorrules                    <- AI instructions for Cursor
  .github/
    copilot-instructions.md       <- AI instructions for GitHub Copilot
```

---

## quikdb.yaml

The project manifest.

```yaml
name: my-app
version: 1.0.0

database:
  primary:
    type: postgres
    migrations: shared/db/migrations/
  cache:
    type: redis

services:
  api-auth:
    type: api
    path: services/api-auth
    port: 8080
    routes:
      - /api/auth/*
    env:
      - DATABASE_URL
      - REDIS_URL
      - JWT_SECRET
      - JWT_SECRET_OLD
      - GOOGLE_CLIENT_ID
      - APPLE_CLIENT_ID
      - TWILIO_SID
      - TWILIO_AUTH_TOKEN

  api-users:
    type: api
    path: services/api-users
    port: 8081
    routes:
      - /api/users/*
    env:
      - DATABASE_URL
      - REDIS_URL
      - JWT_SECRET
      - S3_BUCKET

  api-products:
    type: api
    path: services/api-products
    port: 8082
    routes:
      - /api/products/*
    env:
      - DATABASE_URL
      - REDIS_URL
      - JWT_SECRET

  api-payments:
    type: api
    path: services/api-payments
    port: 8083
    routes:
      - /api/payments/*
      - /webhooks/*
    env:
      - DATABASE_URL
      - REDIS_URL
      - JWT_SECRET
      - STRIPE_SECRET_KEY
      - STRIPE_WEBHOOK_SECRET
      - FLUTTERWAVE_SECRET_KEY
      - FLUTTERWAVE_WEBHOOK_SECRET
      - PAYSTACK_SECRET_KEY

  web:
    type: web
    path: services/web
    port: 3000
    routes:
      - /*
    env:
      - API_URL
      - WS_URL

  ws-gateway:
    type: ws
    path: services/ws-gateway
    port: 8090
    routes:
      - /ws/*
    env:
      - REDIS_URL
      - JWT_SECRET

  ws-chat:
    type: ws
    path: services/ws-chat
    port: 8091
    env:
      - DATABASE_URL
      - REDIS_URL

  ws-notifications:
    type: ws
    path: services/ws-notifications
    port: 8092
    env:
      - DATABASE_URL
      - REDIS_URL
      - FCM_KEY

  worker-email:
    type: worker
    path: services/worker-email
    trigger: queue
    stream: stream:emails
    env:
      - REDIS_URL
      - SENDGRID_KEY
      - SMTP_HOST

  worker-media:
    type: worker
    path: services/worker-media
    trigger: queue
    stream: stream:media
    env:
      - REDIS_URL
      - S3_BUCKET

routing:
  domain: my-app.quikdb.net
  rules:
    - path: /api/auth/*       service: api-auth
    - path: /api/users/*      service: api-users
    - path: /api/products/*   service: api-products
    - path: /api/payments/*   service: api-payments
    - path: /webhooks/*       service: api-payments
    - path: /ws/*             service: ws-gateway
    - path: /*                service: web
```

---

## quikdb.json

Per-service deploy configuration.

```json
{
  "name": "my-app-api-auth",
  "type": "api",
  "buildCommand": "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o app .",
  "startCommand": "./app",
  "envVars": {}
}
```

---

## Dockerfile Conventions

### Go services (api, ws, worker)

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app .

FROM scratch
COPY --from=builder /build/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/app"]
```

Rules:
- Multi-stage build. Build stage: `golang:alpine`. Final stage: `scratch`.
- `CGO_ENABLED=0` for static binary. No libc.
- `-ldflags="-s -w"` strips debug info.
- SSL certs copied from builder (needed for HTTPS calls to external APIs).
- No shell, no package manager, no OS in final image.

### Web service

```dockerfile
FROM node:22-alpine AS builder
WORKDIR /build
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM golang:1.23-alpine AS server
WORKDIR /build
COPY server.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o fileserver .

FROM scratch
COPY --from=server /build/fileserver /fileserver
COPY --from=builder /build/dist /static
COPY --from=server /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 3000
ENTRYPOINT ["/fileserver"]
```

The `server.go` is a ~50 line Go file server with SPA fallback and bot detection for SEO pre-rendering. Node.js is used only at build time to bundle the Preact app. **Node.js does not exist in the production image.**

---

## Authentication

The auth system is a standalone api service (`api-auth`) in `shared/auth/`. It handles all authentication concerns. Every other service validates tokens using the shared JWT middleware.

### JWT Structure

```json
{
  "userId": "uuid",
  "email": "user@example.com",
  "role": "user",
  "iat": 1719849600,
  "exp": 1719853200
}
```

- **Algorithm:** HS256
- **Secret:** `JWT_SECRET` env var
- **Expiry:** 1 hour (access token)
- **Secret rotation:** `JWT_SECRET_OLD` env var. Verification tries new secret first, falls back to old. Zero-downtime rotation.

### Refresh Token

- **Format:** Cryptographically random 256-bit token (not UUID — UUIDs have predictable structure)
- **Expiry:** 90 days
- **Storage:** Hashed in database + cached in Redis with TTL
- **Rotation:** Every refresh issues a new refresh token. Old token stays valid for a 60-second grace period (handles network retries), then is revoked.
- **Revocation:** Delete from Redis. Database hash remains for audit trail.
- **Multiple devices:** Each device gets its own refresh token. Revoking one does not affect others.

### OTP Flow

quikdb-frame provides **delivery integrations** and a **fallback chain runner**. Users configure which providers they use and in what order — the framework handles the retry logic, not the provider selection.

**Built-in delivery integrations:**

| Integration | Channel | Package |
|---|---|---|
| Twilio | SMS, WhatsApp | `shared/messaging/twilio` |
| Vonage | SMS | `shared/messaging/vonage` |
| AWS SNS | SMS | `shared/messaging/sns` |
| Mailgun | Email | `shared/messaging/mailgun` |
| SendGrid | Email | `shared/messaging/sendgrid` |
| AWS SES | Email | `shared/messaging/ses` |

All integrations implement the same `Sender` interface:

```go
type Sender interface {
    Send(ctx context.Context, to string, message string) error
    Channel() string // "sms", "whatsapp", "email"
}
```

**Fallback chain configuration (`config/messaging.yaml`):**

```yaml
otp:
  chain:
    - provider: twilio
      channel: sms
    - provider: vonage
      channel: sms
    - provider: twilio
      channel: whatsapp
    - provider: sendgrid
      channel: email
```

The chain runner tries each provider in order. If one fails, it moves to the next. Users add, remove, or reorder providers as needed. The framework only cares that at least one `Sender` is configured.

**Send OTP (`POST /api/auth/otp/send`):**

1. Validate phone number format (E.164)
2. Rate limit check: max 5 OTP requests per phone per hour. Do not reveal the limit — return success regardless (stealth rate limiting, prevents enumeration).
3. Generate 6-digit OTP, store hashed in database with 10-minute expiry
4. Run the configured fallback chain until one provider succeeds
5. Log delivery attempt (provider used, success/failure, latency)
6. Return `200 {"message": "OTP sent"}` regardless of actual delivery (never reveal whether the phone number exists)

**Verify OTP (`POST /api/auth/otp/verify`):**

1. Rate limit check: max 5 verification attempts per phone per OTP type. After 5 failures, block for 15 minutes.
2. Look up OTP by phone + type, verify not expired
3. Constant-time comparison of OTP codes (prevent timing attacks)
4. On success: delete OTP record, reset rate limit counter, issue tokens
5. On failure: increment attempt counter. Return generic error ("Invalid or expired code").

**Security policies:**
- OTP codes are stored hashed (SHA-256), never plaintext.
- Expired OTPs are cleaned up by a cron worker, not checked at read time only.

### OAuth Flow

quikdb-frame provides OAuth adapters and a common `OAuthProvider` interface. Users configure which providers they need.

**Built-in OAuth adapters:**

| Provider | Package | Token Verification |
|---|---|---|
| Google | `shared/auth/oauth/google` | ID token verification with audience check |
| Apple | `shared/auth/oauth/apple` | JWKS verification (RS256), issuer + audience check |
| GitHub | `shared/auth/oauth/github` | Code-for-token exchange + user API call |
| Facebook | `shared/auth/oauth/facebook` | Token debug endpoint verification |
| Twitter/X | `shared/auth/oauth/twitter` | OAuth 2.0 PKCE flow |
| Discord | `shared/auth/oauth/discord` | Token exchange + user API call |

All adapters implement the same interface:

```go
type OAuthProvider interface {
    VerifyToken(ctx context.Context, token string) (*OAuthUser, error)
    Name() string
}

type OAuthUser struct {
    ProviderID    string
    Email         string
    EmailVerified bool
    Name          string
    AvatarURL     string
}
```

**Configuration (`config/auth.yaml`):**

```yaml
oauth:
  providers:
    - name: google
      clientId: ${GOOGLE_CLIENT_ID}
      clientSecret: ${GOOGLE_CLIENT_SECRET}
    - name: apple
      clientId: ${APPLE_CLIENT_ID}
      teamId: ${APPLE_TEAM_ID}
      keyId: ${APPLE_KEY_ID}
```

**Flow (same for all providers):**

1. Client obtains token from provider's native SDK
2. Client sends token to `POST /api/auth/oauth/{provider}`
3. Framework routes to the configured adapter, which verifies the token
4. Extract user info via the `OAuthUser` struct
5. Find or create user (lookup by email, then provider ID, then create)
6. Issue JWT + refresh token

**Security rules (framework-enforced):**
- `EmailVerified` must be `true`. Unverified emails are rejected.
- Provider ID is checked independently of email (prevents account takeover via email change at provider).
- Profile images from OAuth are downloaded and re-hosted (never hotlink to provider CDNs — they expire).

### Password Auth

- **Hashing:** Argon2id with `memoryCost: 64MB`, `timeCost: 3`, `parallelism: 1`
- **Enhanced format:** Hash `password:userId` (userId acts as a pepper, unique per user)
- **Legacy upgrade:** If a user's hash uses an older algorithm (bcrypt, SHA-256), verify with the old algorithm and re-hash with Argon2id on successful login. Transparent to the user.
- **Brute force protection:** 3 failed attempts → 1-minute block. Next failure after block → 1-hour block. Resets on successful login. Block state stored in Redis with TTL.

### Guest Auth

For mobile apps that need to work before the user signs up:

1. Generate a temporary user ID
2. Issue a JWT with `role: "guest"` and short expiry (24 hours)
3. Guest can browse, add to cart, etc. All data is tied to the guest ID.
4. On signup/login: merge guest data into the real account. Validate ownership via the guest JWT before merging.
5. Guest accounts are cleaned up by a cron worker after 30 days of inactivity.

### API Keys

For service-to-service communication and third-party integrations:

- Generated via `POST /api/auth/apikeys` (authenticated, admin only)
- Format: `qf_live_` prefix + 32 random bytes (base62 encoded)
- Stored hashed (SHA-256) in database. The plaintext key is returned once at creation and never again.
- Scoped by permissions (read, write, admin) and optional IP allowlist.
- Rate limited separately from user auth (higher limits).
- Validated via `X-API-Key` header. The auth middleware checks for API key before JWT.

### Token Validation (All Services)

Every api and ws service uses the shared auth middleware:

```go
func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Check X-API-Key header first
        // 2. Check Authorization: Bearer <jwt> header
        // 3. Verify JWT signature (try JWT_SECRET, fall back to JWT_SECRET_OLD)
        // 4. Check token expiry
        // 5. Set userId in request context
        // 6. Call next handler
    })
}
```

The middleware does NOT query the database. It only verifies the JWT signature and expiry. If you need to check account status (active, suspended, deleted), add a separate middleware that queries the database — but only on sensitive endpoints, not every request.

---

## Payment Gateways

The payment system lives in `shared/payments/` and is used by `api-payments`. It supports multiple providers with automatic routing based on currency and region.

### Supported Providers

| Provider | Currencies | Region | Use Case |
|---|---|---|---|
| **Stripe** | USD, EUR, GBP, CAD, AUD + 135 more | Global | Cards, subscriptions, payouts |
| **Flutterwave** | NGN, GHS, KES, ZAR, UGX, TZS | Africa | Cards, bank transfer, mobile money |
| **Paystack** | NGN, GHS, ZAR, KES | Africa | Cards, bank transfer |
| **PayPal** | USD, EUR, GBP + 25 more | Global | PayPal balance, cards |
| **Lemon Squeezy** | USD, EUR | Global | Digital products, SaaS (Stripe alternative with built-in tax) |
| **Razorpay** | INR | India | Cards, UPI, netbanking |
| **RevenueCat** | All App Store/Play Store currencies | Global | Mobile in-app purchases, subscriptions |

### Payment Router

All providers implement a common interface:

```go
type PaymentProvider interface {
    CreateCharge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
    VerifyWebhook(r *http.Request) (*WebhookEvent, error)
    RefundCharge(ctx context.Context, chargeID string, amount int64) error
    Name() string
}
```

Routing is config-driven. Users define which provider handles which currency/method combination in `config/payments.yaml`:

```yaml
payments:
  providers:
    stripe:
      secretKey: ${STRIPE_SECRET_KEY}
      webhookSecret: ${STRIPE_WEBHOOK_SECRET}
    flutterwave:
      secretKey: ${FLW_SECRET_KEY}
      webhookHash: ${FLW_WEBHOOK_HASH}
    paystack:
      secretKey: ${PAYSTACK_SECRET_KEY}

  routing:
    - currency: NGN
      method: bank_transfer
      provider: flutterwave
    - currency: NGN
      method: card
      provider: paystack
    - currency: INR
      provider: razorpay
    - currency: KES,GHS,ZAR
      provider: flutterwave
    - currency: "*"
      provider: stripe  # default fallback
```

The framework reads this config at startup and routes payments accordingly. Only configured providers are compiled into the binary.

### Webhook Verification

Every provider has a different signature scheme. The shared `webhook.go` handles all of them:

| Provider | Verification Method |
|---|---|
| Stripe | HMAC-SHA256 via `stripe-signature` header. Raw body required — use `express.raw()` or Go `io.ReadAll()` before any parsing. |
| Flutterwave | Static `verif-hash` header comparison + secondary API verification call to confirm transaction. |
| Paystack | HMAC-SHA512 via `x-paystack-signature` header. |
| PayPal | Webhook ID verification via PayPal API call. |
| Lemon Squeezy | HMAC-SHA256 via `X-Signature` header. |
| Razorpay | HMAC-SHA256 of `order_id\|payment_id` via `x-razorpay-signature`. |
| RevenueCat | No cryptographic signature. Structural validation of payload fields only. Endpoint must be undisclosed. |

### Idempotency

Every payment operation must be idempotent. Two layers:

1. **Pre-check:** Before processing, query for existing transaction by provider's event ID / transaction ID. If found, return success without re-processing.
2. **Database constraint:** Unique index on `(providerId, providerEventId)`. Any race condition hits the unique constraint and is caught as a duplicate.

```go
existing := db.FindTransaction(providerEventId)
if existing != nil {
    return existing, nil // Already processed
}
// Process payment...
err := db.InsertTransaction(tx) // Unique index catches races
if isDuplicateKeyError(err) {
    return db.FindTransaction(providerEventId), nil
}
```

### Transaction Ledger

Every financial operation creates an immutable ledger entry with a post-transaction balance snapshot:

```go
type Transaction struct {
    ID              string
    UserID          string
    Type            string    // CREDIT, DEBIT, REFUND, HOLD, RELEASE
    Amount          int64     // In minor units (cents, kobo)
    Currency        string
    Provider        string    // stripe, flutterwave, etc.
    ProviderEventID string    // Unique index
    Status          string    // PENDING, COMPLETED, FAILED
    BalanceAfter    int64     // Snapshot of balance after this transaction
    Metadata        map[string]string
    CreatedAt       time.Time
}
```

The `BalanceAfter` field means you can reconstruct the balance at any point in history without re-summing all transactions.

### Subscription Lifecycle

```
created → active → past_due → grace_period → expired → cancelled
                  → cancelled (voluntary)
```

| Event | Action |
|---|---|
| Payment succeeded | Set `status: active`, clear retry counter |
| Payment failed | Increment retry counter. After 4 retries: enter grace period. |
| Grace period started | User retains access for N days (configurable). Send warning emails. |
| Grace period expired | Downgrade to free tier. |
| Cancelled by user | Mark `autoRenew: false`. Access continues until `expiresAt`. |
| Subscription deleted | Immediate downgrade to free tier. |

**Polling fallback:** A cron worker polls the payment provider every 5 minutes to catch missed webhooks. If the poller finds a state mismatch, it syncs and sends an alert.

---

## Database & Connection Management

quikdb-frame provides database adapters with connection pooling, health checks, and retry logic. Users choose their database — the framework provides the wiring.

### Built-in Database Adapters

| Database | Driver | Package |
|---|---|---|
| PostgreSQL | `pgx` (pure Go) | `shared/db/postgres` |
| MongoDB | `mongo-driver` | `shared/db/mongo` |
| MySQL | `go-sql-driver/mysql` | `shared/db/mysql` |
| SQLite | `modernc.org/sqlite` (pure Go, no CGO) | `shared/db/sqlite` |

All adapters implement:

```go
type Database interface {
    Connect(ctx context.Context) error
    Ping(ctx context.Context) error
    Close(ctx context.Context) error
    Health() string // "connected" or "disconnected"
}
```

**Configuration (`config/database.yaml`):**

```yaml
database:
  adapter: postgres  # or mongo, mysql, sqlite
  url: ${DATABASE_URL}
  pool:
    maxConns: 20
    minConns: 2
    maxConnLifetime: 1h
    maxConnIdleTime: 30m
    healthCheckPeriod: 1m
```

### Connection Behavior

- **Health check:** The `GET /health` endpoint calls `db.Ping()`. If it fails, return `503`. This ensures the load balancer routes traffic away from unhealthy instances.
- **Retry:** Exponential backoff on connection failure (100ms, 200ms, 400ms, 800ms, max 5 attempts).
- **Graceful shutdown:** On SIGTERM, stop accepting new requests, drain in-flight requests (30s timeout), close database pool, exit.

### Redis

Used for three purposes:

1. **Caching** — Cache-aside pattern with configurable TTL per resource type
2. **Queuing** — Redis Streams for event routing and worker job queues
3. **Sessions** — Refresh tokens, rate limit counters, distributed locks

Connection uses the same pool pattern with retry and health check.

### Migrations

SQL migration files in `shared/db/migrations/`. Numbered, up/down pairs.

```
001_create_users.up.sql
001_create_users.down.sql
002_create_products.up.sql
002_create_products.down.sql
```

Migrations run via CLI (`quikdb-frame migrate up`), never on service startup. This prevents migration races when multiple replicas start simultaneously.

---

## Caching

Cache-aside pattern built into the shared library:

```go
func GetUser(ctx context.Context, userID string) (*User, error) {
    // 1. Check Redis
    cached, err := cache.Get(ctx, "user:"+userID)
    if err == nil {
        return cached.(*User), nil
    }
    // 2. Query database
    user, err := db.FindUser(ctx, userID)
    if err != nil {
        return nil, err
    }
    // 3. Store in Redis with TTL
    cache.Set(ctx, "user:"+userID, user, 5*time.Minute)
    return user, nil
}
```

Cache invalidation: on any write to a resource, delete its cache key. The next read populates it fresh.

---

## Notifications

quikdb-frame provides notification adapters across push, email, and SMS channels. Users configure which providers they use. All channels share the same fallback chain pattern from the messaging system.

### Push Notifications

**Built-in push adapters:**

| Provider | Platform | Package |
|---|---|---|
| FCM (Firebase) | Android, Web | `shared/notify/fcm` |
| APNs | iOS | `shared/notify/apns` |
| Expo Push | Expo (iOS + Android) | `shared/notify/expo` |
| OneSignal | All | `shared/notify/onesignal` |

All adapters implement:

```go
type PushSender interface {
    Send(ctx context.Context, token string, title string, body string, data map[string]string) error
    Platform() string // "ios", "android", "web"
}
```

The framework routes to the correct adapter based on the device token's platform. Users configure which adapters are active.

### Email

**Built-in email adapters:**

| Provider | Package |
|---|---|
| SendGrid | `shared/messaging/sendgrid` |
| AWS SES | `shared/messaging/ses` |
| Mailgun | `shared/messaging/mailgun` |
| Postmark | `shared/messaging/postmark` |
| SMTP | `shared/messaging/smtp` |

Configured via the same `config/messaging.yaml` fallback chain. HTML templates in `worker-email/templates/`, rendered with Go's `html/template`.

### SMS

Uses the same `Sender` interface and fallback chain as OTP delivery (see Authentication section). No separate SMS configuration — messaging providers are configured once and shared across OTP, transactional SMS, and any other SMS use case.

---

## Observability

### Structured Logging

Every service logs JSON to stdout:

```json
{
  "level": "info",
  "msg": "request completed",
  "requestId": "req-abc123",
  "method": "POST",
  "path": "/api/auth/login",
  "status": 200,
  "duration_ms": 42,
  "userId": "user-xyz",
  "ts": "2026-06-07T10:30:00Z"
}
```

### Request ID Propagation

Every incoming request gets a unique `X-Request-Id` header (generated if not present). This ID is:
- Logged with every log line
- Passed to downstream service calls
- Returned in the response headers

### Health Checks

Every service that serves HTTP has `GET /health`:

```json
{
  "status": "ok",
  "version": "1.0.0",
  "uptime": "2h34m",
  "db": "connected",
  "redis": "connected"
}
```

Returns `503` if any critical dependency is down.

### Graceful Shutdown

On SIGTERM:
1. Stop accepting new connections
2. Return `503` on health check (tells load balancer to stop routing)
3. Wait for in-flight requests to complete (30s timeout)
4. Close database and Redis connections
5. Exit

---

## Resilience

### Rate Limiting

Per-user and per-IP rate limiting using Redis:

```go
func RateLimit(key string, max int, window time.Duration) bool {
    count := redis.Incr(key)
    if count == 1 {
        redis.Expire(key, window)
    }
    return count <= max
}
```

**Configuration (`config/ratelimit.yaml`):**

```yaml
ratelimit:
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
```

Users define their own rules, keys (ip, user, api-key), limits, and windows. The framework provides the middleware and Redis-backed counter — not the policy.

### Circuit Breaker

For external API calls (payment providers, OAuth providers, notification services):

```go
breaker := circuitbreaker.New(circuitbreaker.Config{
    MaxFailures:  5,
    Timeout:      30 * time.Second,
    HalfOpenMax:  2,
})
result, err := breaker.Execute(func() (interface{}, error) {
    return stripe.CreateCharge(amount, currency)
})
```

### Distributed Locks

For operations that must not run concurrently (payment processing, balance updates):

```go
lock := redis.Lock("payment:"+orderID, 30*time.Second)
if !lock.Acquired() {
    return ErrAlreadyProcessing
}
defer lock.Release()
// Process payment...
```

### Optimistic Locking

For concurrent database writes (like wallet balance updates):

```sql
UPDATE wallets SET balance = balance - $1, version = version + 1
WHERE user_id = $2 AND version = $3
```

If `version` doesn't match, the update affects 0 rows and the operation retries.

---

## CLI Reference

### Installation

```bash
# npm (widest reach)
npm install -g quikdb-frame

# Homebrew (macOS)
brew install quikdb/tap/quikdb-frame

# Go install
go install github.com/quikdb/quikdb-frame@latest

# curl (universal)
curl -fsSL https://frame.quikdb.com/install | sh

# GitHub Releases: pre-built binaries for linux/mac/windows amd64/arm64
```

The CLI is written in Go (single binary, dogfooding our own principles).

### Project Commands

```bash
# Scaffold a new project
quikdb-frame init my-app
# Interactive: project description, database type, which services to include

# Add a service
quikdb-frame add api orders
quikdb-frame add worker media-processor
quikdb-frame add ws live

# Run all services locally with hot reload
quikdb-frame dev

# Run a single service
quikdb-frame dev api-auth

# Database migrations
quikdb-frame migrate up
quikdb-frame migrate down
quikdb-frame migrate create add_orders_table
```

### Deploy Commands

```bash
# Deploy all services to QuikDB
quikdb-frame deploy

# Deploy a single service
quikdb-frame deploy api-auth

# Check deployment status
quikdb-frame status

# View logs
quikdb-frame logs api-auth
quikdb-frame logs ws-gateway --follow
```

### Convert Commands

```bash
# Convert existing project to quikdb-frame
quikdb-frame convert ./my-express-app --from express

# Supported source frameworks:
#   express, nestjs, nextjs, fastapi, django, flask, gin, fiber, spring
```

### Generate Commands

```bash
# Generate a resource (model + migration + routes + handlers)
quikdb-frame generate resource product

# Generate a single endpoint
quikdb-frame generate endpoint POST /api/payments/checkout

# Generate a payment integration
quikdb-frame generate payment stripe
quikdb-frame generate payment flutterwave
```

---

## AI Integration

quikdb-frame does not ship AI. Every scaffolded project includes instruction files that teach any AI coding assistant the rules:

### CLAUDE.md

```markdown
# quikdb-frame project

## Architecture
This is a multi-service project. Each service is a Go binary in services/.
Shared code lives in shared/. Services import shared packages.

## Service rules
- api services: REST endpoints + business logic. One domain per service.
- web service: Preact frontend. Pure client-side. No SSR with data fetching.
- ws services: WebSocket. Gateway routes events, workers process them.
- worker services: Background jobs. Queue consumers or cron.

## Strict rules
- All Go services: CGO_ENABLED=0, single static binary, scratch Docker image
- All services read PORT from environment
- All HTTP services have GET /health returning JSON with db status
- NO node_modules in production images. Node is build-time only for web.
- NO interpreted runtimes in production containers
- Database access through shared/db/ connection helpers only
- Auth tokens generated by api-auth, validated by shared/auth/middleware.go
- Payment webhooks must verify signatures before processing
- All financial operations must be idempotent
- All database writes must use transactions where atomicity is needed
- Graceful shutdown on SIGTERM in every service
```

### .cursorrules and .github/copilot-instructions.md

Same rules, formatted for each tool's convention.

---

## Converter Spec

`quikdb-frame convert` takes an existing project and produces a quikdb-frame project.

### Process

1. **Scan** — Detect source framework, enumerate routes, middleware, models, WebSocket handlers, static assets, env vars, database connections
2. **Plan** — Determine which quikdb-frame service types are needed. Default output is a single `api` service. If the source project has 15+ routes across distinct domains, suggest splitting — but do not force it.
3. **Generate** — Create quikdb-frame project with equivalent Go code for each service. Frontend code converted to Preact.
4. **Verify** — Compare endpoint count, run both apps, hit every endpoint, diff responses.

### Conversion Mapping

| Source Concept | Target Service | Location |
|---|---|---|
| REST routes/controllers | api-* | `services/api-*/handlers/` |
| Middleware (auth, cors, validation) | shared + api-* | `shared/auth/`, `services/api-*/middleware/` |
| Database models/schemas | shared | `shared/types/` + `shared/db/migrations/` |
| WebSocket handlers | ws-* | `services/ws-*/handlers/` |
| Background jobs/cron | worker-* | `services/worker-*/` |
| React/Vue/Angular pages | web | `services/web/src/pages/` (converted to Preact) |
| Static files | web | `services/web/public/` |
| Environment variables | all | `.env.example` + `quikdb.yaml` |

### Supported Source Frameworks (Priority)

| Priority | Framework | Language |
|---|---|---|
| P0 | Express | JavaScript/TypeScript |
| P0 | Next.js (API routes + pages) | TypeScript |
| P1 | NestJS | TypeScript |
| P1 | FastAPI | Python |
| P2 | Django REST | Python |
| P2 | Flask | Python |
| P2 | Gin / Fiber | Go |
| P3 | Spring Boot | Java |
| P3 | Laravel | PHP |

---

## Size Targets

| Service Type | Docker Image | RAM Idle | Cold Start |
|---|---|---|---|
| api | < 15MB | < 10MB | < 200ms |
| web | < 20MB | < 10MB | < 100ms |
| ws | < 10MB | < 10MB | < 100ms |
| worker | < 10MB | < 10MB | N/A |

Comparison with popular frameworks (same app):

| Stack | Docker Image | RAM Idle | Cold Start |
|---|---|---|---|
| Next.js | 300-800MB | 80-150MB | 2-5s |
| NestJS | 200-400MB | 60-100MB | 2-3s |
| Django | 150-300MB | 40-80MB | 1-2s |
| Spring Boot | 200-500MB | 100-200MB | 5-15s |
| **quikdb-frame (total, all services)** | **60-100MB** | **30-50MB** | **< 200ms per service** |

A full quikdb-frame app with 4 api services, 1 web service, 2 ws services, and 2 workers uses less total resources than a single Next.js app.

---

## Distribution & Awareness

quikdb-frame is the operating system for QuikDB Compute. It lives on the dashboard as the default way to build — not a separate product, but the layer that makes everything work.

### Where It Lives

- **GitHub:** `github.com/quikdb/quikdb-frame` — MIT license, open source
- **QuikDB Compute dashboard:** Integrated into the deploy wizard. "Build with quikdb-frame" is the first option.
- **npm / Homebrew / Go install / curl:** CLI distribution
- **docs.quikdb.com/frame:** Documentation, guides, API reference

### Awareness Channels

- **Template gallery on Compute dashboard** — community-contributed starters (SaaS, e-commerce, chat app, blog, API-only)
- **Benchmarks** — published, reproducible comparisons vs Next.js, NestJS, Django. Image size, cold start, RAM, requests/second.
- **"Built with quikdb-frame" badge** — optional `X-Powered-By: quikdb-frame` response header
- **dev.to / Hashnode articles** — "How we reduced our Docker image from 300MB to 12MB"
- **Bounty program** — cash rewards for converter plugins, templates, and core contributions
- **Discord community** — `#quikdb-frame` channel
- **`#BuildQuik` hashtag** — anyone who ships with quikdb-frame tags their posts

### Growth Loop

Developer discovers QuikDB Compute → deploys a Next.js app → sees it uses 300MB and cold starts in 3 seconds → dashboard suggests quikdb-frame → converts their app → 12MB image, 50ms cold start → posts about it → more developers discover QuikDB.

---

## What quikdb-frame Is

quikdb-frame is the **operating system for QuikDB applications**. Not a framework — frameworks handle routing and middleware. quikdb-frame defines the full lifecycle: how apps are structured, built, deployed, scaled, observed, and maintained. It is to QuikDB what iOS is to iPhone — the layer that makes everything work together.

## What quikdb-frame Is Not

- **Not a framework.** Frameworks give you a router. quikdb-frame gives you structure, deployment, auth, payments, observability, scaling, and workflow. It is bigger than a framework.
- **Not a new programming language.** It generates Go, but you don't need to know Go. Your AI writes it.
- **Not an AI product.** It works with any AI tool. It also works without AI — you can write the Go code yourself.
- **Not locked to QuikDB.** The output is standard Docker containers. Deploy anywhere. But QuikDB deployment is one command.
- **Not a monolith and not forced microservices.** Start with one api + one web. Split when your app needs it. The tooling supports both.
- **Not a place for node_modules.** No interpreted runtime, no package manager, no dependency tree ships to production. Ever.
