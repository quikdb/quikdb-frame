# quikdb-frame

The operating system for QuikDB applications.

quikdb-frame defines how apps are structured, built, deployed, scaled, and observed on [QuikDB Compute](https://compute.quikdb.com). It is not a framework — it is bigger than a framework.

## What It Does

| What | How |
|---|---|
| **Structure** | Opinionated project layout with shared code and independent services |
| **Build** | Compiles Go services into static binaries. No runtime, no interpreter. |
| **Deploy** | One command to deploy all services to QuikDB Compute |
| **Auth** | JWT, OAuth, OTP, API keys — built-in adapters, configurable providers |
| **Payments** | 7 payment gateways with config-driven currency routing |
| **Observability** | Structured logging, request ID propagation, health checks |
| **Real-time** | Native WebSocket with gateway + worker architecture |

## Why

| Metric | Next.js / NestJS | quikdb-frame |
|---|---|---|
| Docker image | 200-800 MB | < 15 MB per service |
| RAM at idle | 60-150 MB | < 10 MB per service |
| Cold start | 2-5 seconds | < 200 ms |
| Dependencies in prod | 500+ MB node_modules | Zero. Single binary. |

A full quikdb-frame app (4 API services, 1 web, 2 WebSocket, 2 workers) uses less total resources than a single Next.js app.

## Quick Start

```bash
# Install the CLI
npm install -g quikdb-frame

# Create a new project
quikdb-frame init my-app

# Run locally with hot reload
cd my-app && quikdb-frame dev

# Deploy to QuikDB Compute
quikdb-frame deploy
```

## How It Works

A new project starts simple:

```
my-app/
  quikdb.yaml           # Project manifest
  shared/               # Shared code (auth, db, types)
  services/
    api/                 # Single API service
    web/                 # Preact frontend
  CLAUDE.md              # AI assistant instructions
  .cursorrules           # Cursor instructions
```

As your app grows, split services:

```bash
quikdb-frame add api auth       # Extract auth into its own service
quikdb-frame add api payments   # Add a payments service
quikdb-frame add ws chat        # Add WebSocket chat
quikdb-frame add worker email   # Add email background worker
```

Each service compiles to a single static Go binary. No node_modules. No pip packages. No interpreted runtime in production. Ever.

## Principles

1. **Compiled over interpreted.** Static Go binaries. No runtime in production.
2. **Zero dependency bleed.** Nothing ships to production except the binary.
3. **Start simple, split when ready.** Begin with `api` + `web`. Split when you need to.
4. **Stateless services.** State lives in the database. Containers restart anytime.
5. **AI-assisted, not AI-dependent.** Works with Claude Code, Cursor, Copilot, or no AI at all.
6. **Deploy-ready from scaffold.** Fresh projects deploy without editing a single file.
7. **Production-first.** Auth, health checks, graceful shutdown, logging, rate limiting from the first commit.

## Convert Existing Apps

Already have an Express, Next.js, or NestJS app? Convert it:

```bash
quikdb-frame convert ./my-express-app --from express

# Scanning project...
# Found: 12 routes, 4 middleware, 3 models
# Converting...
# Done. Output: ./my-express-app-converted/
#
# Before: 340MB image, 2.5s cold start, 80MB RAM
# After:  11MB image, 60ms cold start, 6MB RAM
```

Supported sources: Express, Next.js, NestJS, FastAPI, Django, Flask, Gin, Fiber, Spring Boot, Laravel.

## Built-in Integrations

quikdb-frame provides adapters — you choose which ones to use.

**Auth:** Google, Apple, GitHub, Facebook, Twitter/X, Discord OAuth + Twilio, Vonage, SNS for OTP

**Payments:** Stripe, Flutterwave, Paystack, PayPal, Lemon Squeezy, Razorpay, RevenueCat

**Notifications:** FCM, APNs, Expo Push, OneSignal + SendGrid, SES, Mailgun, Postmark, SMTP

**Database:** PostgreSQL, MongoDB, MySQL, SQLite

All integrations follow the same pattern: a common interface, multiple adapters, config-driven selection.

## Documentation

- [Full Specification](SPEC.md) — the complete technical spec
- [Contributing Guide](CONTRIBUTING.md) — how to contribute
- [Code of Conduct](CODE_OF_CONDUCT.md) — community standards

## License

MIT License. See [LICENSE](LICENSE).

## Contributing

We welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Areas We Need Help

- **Converter plugins** — add support for more source frameworks
- **Adapters** — new payment, auth, or notification provider adapters
- **Templates** — starter projects (SaaS, e-commerce, chat app, blog)
- **Documentation** — guides, tutorials, examples
- **Testing** — test coverage for the CLI, converters, and adapters

## Community

- [GitHub Issues](https://github.com/quikdb/quikdb-frame/issues) — bugs and feature requests
- [Telegram](https://t.me/quikdb) — join and find the discussion group on the pinned post
- [X / Twitter](https://x.com/quikdb_online) — follow for updates

Built by the [QuikDB](https://quikdb.com) team.
