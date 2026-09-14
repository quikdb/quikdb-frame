# quikdb-frame

The operating system for QuikDB applications.

quikdb-frame defines how apps are structured, built, deployed, scaled, and observed on [QuikDB Compute](https://compute.quikdb.com). It is not a framework — it is bigger than a framework.

Implementation is in progress. [Capability ledger](CAPABILITIES.md) records the source and
acceptance gate for every specification section. [Deployment contracts](DOCS.md) describe the
current CLI behavior. Adapters and automatic business-logic conversion remain under development.

## What It Does

| What | How |
|---|---|
| **Structure** | Opinionated project layout with shared code and independent services |
| **Build** | Compiles Go services into static binaries. No runtime, no interpreter. |
| **Deploy** | One command to deploy all services to QuikDB Compute |
| **Auth** | Generated JWT examples; complete provider adapters are planned |
| **Payments** | Provider adapter suite is planned |
| **Observability** | Structured logging, request ID propagation, health checks |
| **Real-time** | Native WebSocket with gateway + worker architecture |

## Size targets

Go services use multi-stage builds with a static binary and CA certificates in a scratch image.
The web service builds frontend assets separately and serves them with Go. Size, memory and
startup goals are in [SPEC.md](SPEC.md#size-targets); measure each real application before
claiming savings. Cross-framework business logic and dependencies can change those results.

## Quick Start

```bash
# Install a verified release without Go (macOS/Linux; default: ~/.local/bin)
curl -fsSL https://github.com/quikdb/quikdb-frame/releases/latest/download/install.sh -o /tmp/quikdb-frame-install.sh
sh /tmp/quikdb-frame-install.sh --verify-provenance # requires gh; omit for checksum-only verification
export PATH="$HOME/.local/bin:$PATH"

# Create a new project
quikdb-frame init my-app

# Run locally with hot reload
cd my-app && quikdb-frame dev

# Push to GitHub first, then deploy to QuikDB Compute
quikdb-frame deploy
```

`deploy` creates services, observes active builds and retries failed/stopped/sleeping services
using the same deployment ID. Once live, push to the connected branch for Git auto-deployment.
From v0.1.11, the CLI also deploys existing GitHub applications as-is. Deployment-time
conversion remains unavailable until preservation checks pass.

## Deploy an existing application

From the existing application's Git working tree, `quikdb-frame deploy` uses its origin and
current branch. No Frame manifest or source rewrite is required. To deploy another repository:

```text
quikdb-frame deploy --repo https://github.com/your-team/your-app --branch main --mode as-is --dry-run
quikdb-frame deploy --repo https://github.com/your-team/your-app --branch main --mode as-is
```

Compute detects the original runtime and build/start settings, including its Dockerfile. Set
required runtime variables in Compute explicitly. Private repositories require the same Git
connection used by the dashboard. Only committed and pushed source is deployed; this first path
supports GitHub, not local uploads or arbitrary host/runtime requirements.

For a monorepo service, supply `--subdirectory apps/api --config deployment.json`. The JSON object
contains the existing Compute configuration fields (`appType`, `configSource`, original
`buildCommand`/`startCommand`, and actual `port`; resource/env fields are explicit). Detection
currently reads the repository root and will require this explicit service configuration.
`--port` overrides both port fields. The dry-run prints source/runtime/port metadata without
printing configuration commands or environment values. As-is `--json` emits a result object;
errors go to stderr with a nonzero exit. Put flags before an optional native Frame service name.

`--mode frame` works for native Frame projects. Existing non-Frame source is blocked before
submission because automatic business-logic conversion is not certified yet. This release does
not offer managed storage for stateful databases/files or change their schemas/data.

## CLI sign-in

Download the binary for your platform and `SHA256SUMS` from the same tagged
[release](https://github.com/quikdb/quikdb-frame/releases/latest). Verify its SHA-256 checksum
before installing it on your PATH. From v0.1.10, releases include signed build provenance;
verify it with `gh attestation verify <binary> --repo quikdb/quikdb-frame --signer-workflow
quikdb/quikdb-frame/.github/workflows/release.yml`. The CLI upgrade currently verifies checksums;
automatic in-CLI provenance verification remains planned. The macOS/Linux installer verifies
checksums and can require signed provenance with `--verify-provenance`. Pin a version with
`--version v0.1.11`; set `QUIKDB_FRAME_INSTALL_DIR` for a custom installation directory. Connect through Compute:

```text
quikdb-frame login
quikdb-frame whoami
quikdb-frame deploy
quikdb-frame logout
```

Approve the terminal in Compute using your existing dashboard account, or sign in with an email
verification code. A remote/headless terminal uses `quikdb-frame login --device`: open the displayed
link on another device and enter the terminal code. Approve only a login you started.

Access lasts 15 minutes and refreshes automatically within a 30-day session. Logout revokes that
session. macOS/Windows use native credential storage; Linux uses Secret Service where available
or a private 0700 directory/0600 file in `~/.quikdb-frame` on headless systems. Desktop storage
failures do not fall back to plaintext. `QUIKDB_FRAME_CONFIG_DIR` can select an absolute private
fallback/lock directory for a headless workspace; it does not change the desktop keyring account.
Older unverified/expired saved tokens require a new login.

For an existing verified user token in CI, supply `QUIKDB_TOKEN` through your CI secret store;
commands verify it without saving it. A dedicated scoped CI-token lifecycle is still planned.
Never put tokens in URLs or commit them. Node/operator tokens belong to the separate node CLI.

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

Native Frame services compile to Go binaries; frontend dependencies are used during asset builds.
Existing applications keep their original runtime when deployed as-is.

## Principles

1. **Compiled over interpreted.** Static Go binaries. No runtime in production.
2. **Zero dependency bleed.** Nothing ships to production except the binary.
3. **Start simple, split when ready.** Begin with `api` + `web`. Split when you need to.
4. **Stateless services.** State lives in the database. Containers restart anytime.
5. **AI-assisted, not AI-dependent.** Works with Claude Code, Cursor, Copilot, or no AI at all.
6. **Deploy-ready from scaffold.** Fresh projects deploy without editing a single file.
7. **Production-first.** Auth, health checks, graceful shutdown, logging, rate limiting from the first commit.

## Convert Existing Apps

The current Express/Flask command scans routes and generates a migration scaffold with
handler stubs. It does **not** preserve existing business logic. Review and implement every
handler before use; automatic conversion will be released only for independently tested subsets.

```bash
quikdb-frame convert ./my-express-app --from express

# Output contains handler stubs that require manual implementation.
```

Route-scaffold scanners: Express and Flask. Other framework converters in SPEC.md are planned.

## Built-in Integrations

The following adapter catalog is planned and tracked in [CAPABILITIES.md](CAPABILITIES.md).
These providers are not all implemented by the generated application.

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
