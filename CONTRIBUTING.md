# Contributing to quikdb-frame

Thank you for your interest in contributing to quikdb-frame. This document explains how to get involved.

## How to Contribute

### Reporting Bugs

Open a [GitHub Issue](https://github.com/quikdb/quikdb-frame/issues/new?template=bug_report.md) with:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Your environment (OS, Go version, CLI version)

### Suggesting Features

Open a [GitHub Issue](https://github.com/quikdb/quikdb-frame/issues/new?template=feature_request.md) with:

- The problem you're trying to solve
- Your proposed solution
- Any alternatives you considered

### Submitting Code

1. Fork the repository
2. Create a feature branch from `main`: `git checkout -b feat/my-feature`
3. Make your changes
4. Write or update tests
5. Run the test suite: `go test ./...`
6. Commit with a clear message (see commit conventions below)
7. Push to your fork
8. Open a Pull Request against `main`

## What to Work On

### High-Impact Areas

| Area | Description | Difficulty |
|---|---|---|
| **Converter plugins** | Add support for new source frameworks (Flask, Laravel, Spring) | Medium |
| **Payment adapters** | New payment provider implementations | Medium |
| **Auth adapters** | New OAuth provider implementations | Easy |
| **Notification adapters** | New push/email/SMS provider implementations | Easy |
| **Database adapters** | New database driver implementations | Medium |
| **Templates** | Starter project templates (SaaS, e-commerce, chat) | Easy |
| **CLI commands** | New CLI features or improvements | Medium |
| **Documentation** | Guides, tutorials, examples | Easy |
| **Tests** | Test coverage for CLI, converters, adapters | Medium |

### Labels

- `good first issue` — good for newcomers
- `help wanted` — we need community help
- `adapter` — new integration adapter
- `converter` — converter plugin work
- `template` — starter template
- `cli` — CLI tool changes
- `docs` — documentation improvements

## Development Setup

### Prerequisites

- Go 1.23+
- Node.js 22+ (for web service builds only)
- Docker (for testing image builds)
- Git

### Getting Started

```bash
git clone https://github.com/quikdb/quikdb-frame.git
cd quikdb-frame
go mod download
go test ./...
```

### Project Layout

```
quikdb-frame/
  cmd/                  # CLI entry points
  internal/
    scaffold/           # Project scaffolding logic
    convert/            # Converter plugins
    adapters/           # Payment, auth, notification adapters
    config/             # Config file parsing
  templates/            # Project templates (embedded)
  SPEC.md               # Full specification
```

## Code Standards

### Go Code

- Follow standard Go conventions (`gofmt`, `go vet`, `golint`)
- All exported functions must have doc comments
- Error messages should be lowercase, no trailing punctuation
- Use `context.Context` as the first parameter where applicable
- No global state — pass dependencies explicitly

### Adapters

Every adapter must implement the relevant interface from the spec:

- Payment: `PaymentProvider`
- OAuth: `OAuthProvider`
- Messaging: `Sender`
- Push: `PushSender`
- Database: `Database`

Every adapter must include:

- Implementation file (e.g., `stripe.go`)
- Test file (e.g., `stripe_test.go`)
- Config example in the README or doc comment

### Converters

Converter plugins must:

- Detect the source framework automatically
- Map all routes, middleware, models, and WebSocket handlers
- Preserve all environment variables
- Generate valid quikdb-frame project structure
- Include before/after comparison output

## Commit Conventions

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Razorpay payment adapter
fix: correct JWT expiry calculation in auth middleware
docs: add quick start guide for Flask converter
test: add integration tests for Stripe webhook verification
chore: update Go dependencies
```

### Scope (optional)

```
feat(payments): add Razorpay adapter
fix(auth): correct JWT expiry calculation
docs(converter): add Flask conversion guide
```

## Pull Request Process

1. PRs must target the `main` branch
2. All tests must pass
3. New adapters/converters must include tests
4. PR description must explain what changed and why
5. One approval required for merge
6. Squash merge is preferred

## Adding a New Adapter

Example: adding a new payment provider.

1. Create `internal/adapters/payments/newprovider.go`
2. Implement the `PaymentProvider` interface
3. Create `internal/adapters/payments/newprovider_test.go`
4. Add the provider to the config parser in `internal/config/payments.go`
5. Add documentation in a doc comment or example
6. Open a PR

```go
// internal/adapters/payments/newprovider.go
package payments

type NewProvider struct {
    secretKey string
}

func (p *NewProvider) CreateCharge(ctx context.Context, req ChargeRequest) (*ChargeResult, error) {
    // Implementation
}

func (p *NewProvider) VerifyWebhook(r *http.Request) (*WebhookEvent, error) {
    // Implementation
}

func (p *NewProvider) RefundCharge(ctx context.Context, chargeID string, amount int64) error {
    // Implementation
}

func (p *NewProvider) Name() string {
    return "newprovider"
}
```

## Adding a New Converter

1. Create `internal/convert/framework.go` (e.g., `flask.go`)
2. Implement the scanner, planner, and generator
3. Create test fixtures in `internal/convert/testdata/flask/`
4. Add the framework to the CLI's `--from` flag options
5. Open a PR

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to uphold this code.

## Questions?

- Open a [Discussion](https://github.com/quikdb/quikdb-frame/discussions) on GitHub
- Join [Telegram](https://t.me/quikdb) and find the discussion group on the pinned post
- Tag [@quikdb_online](https://x.com/quikdb_online) on X
