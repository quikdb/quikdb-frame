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

`deploy` creates supported services, observes active builds and retries failed/stopped/sleeping
services using the same deployment ID. Once live, push to the connected branch for Git
auto-deployment. From v0.1.11, the CLI also deploys existing GitHub applications as-is.
Root-context native Frame projects require the pending Compute build-context contract described
below; the CLI refuses to submit them through an older API/runner. v0.1.16 adds offline local
archive review and one bounded Express conversion pilot. Live archive submission remains blocked
until Compute advertises the complete qualified capability.

## Deploy an existing application

v0.1.13 adds [ID-based app management](docs/CLI_MANAGEMENT.md): status/inspect,
logs/history, lifecycle, settings/resources, env and domains use the same dashboard APIs.
Older sessions need logout/new login to explicitly approve environment and domain access.

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

From v0.1.14, a monorepo service uses `--subdirectory apps/api`. Shared detection reads
that directory, preserves explicit `quikdb.json` commands/runtime/port/health settings, and
fails clearly when the repository/ref/service is unavailable. Compute's repository wizard has
the same optional Service directory choice. Custom Dockerfile/target and cross-directory
build contexts remain under development.

Use `--config deployment.json` to supply explicit existing Compute settings (`appType`,
`configSource`, original `buildCommand`/`startCommand`, actual `port`, explicit resource/env
fields). `--port` overrides both port fields. The dry-run prints source/runtime/port metadata
without configuration commands or environment values. As-is `--json` emits a result object;
errors go to stderr with a nonzero exit. Put flags before an optional native Frame service name.

From v0.1.16, `--mode frame` keeps native Frame behavior for Frame projects. For local source, it must be paired
with an explicit `--from` converter. The only qualified pilot is the bounded Express subset below;
unsupported or ambiguous code stops before login, upload or deployment and never falls back
automatically. Repository conversion remains unavailable. Native services whose Docker context
differs from their service directory also stop before submission until the Compute API and runner
can carry context, Dockerfile and target separately. This release does not offer managed storage
for stateful databases/files or change their schemas/data.

## Validate a deployment manifest

From v0.1.15, a single-service `quikdb.json` can opt into `schemaVersion: 1`.
The [schema](contracts/deployment-manifest-v1.schema.json) and
[example](contracts/deployment-manifest-v1.example.json) define runtime, explicit original
install/build/start commands and port. Empty install/build commands are intentional when no
such step is required. Optional framework and health check default to empty and `/`.
Runtime versions remain subject to platform support; Node versions are major-only.

```text
quikdb-frame manifest validate --file quikdb.json --json
quikdb-frame deploy --repo https://github.com/your-team/your-app --branch main --mode as-is --config quikdb.json --dry-run --json
```

Validation runs offline without login and emits metadata only. Unknown versions/fields and
embedded environment values are rejected. Set secrets explicitly through Compute or CLI env
commands. Commit/push the manifest in the selected service directory for shared API/dashboard
detection; an explicit local `--config` applies only to that invocation. `--port` overrides its
port. Saved settings/redeploy precedence is still being completed.

Unversioned repository manifests and explicit Compute `--config` objects remain readable.
The offline validator certifies v1 structure, not runtime compatibility, deployment success or
conversion. Native multi-service `quikdb.yaml` is a separate format.

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

### Managed databases

```text
quikdb-frame db list
quikdb-frame db tables <database-id>
quikdb-frame db connect <database-id>
quikdb-frame db query <database-id> --file query.sql
quikdb-frame db dump <database-id> --output backup.sql
SOURCE_DATABASE_URL='postgresql://…' quikdb-frame db migrate <database-id> --source-env SOURCE_DATABASE_URL
quikdb-frame db migrate <database-id> --file backup.sql
```

These commands connect through QuikDB. The CLI never receives or prints the underlying provider,
host or credential. Source URLs use an environment variable so they do not enter shell history or
the process list. New logins request a separate managed-database permission; older sessions must
log out and sign in again before using these commands. Database deletion and billing are excluded.

For an existing verified user token in CI, supply `QUIKDB_TOKEN` through your CI secret store;
commands verify it without saving it. A dedicated scoped CI-token lifecycle is still planned.
Never put tokens in URLs or commit them. Node/operator tokens belong to the separate node CLI.

## How It Works

A new project starts simple:

```
my-app/
  quikdb.yaml           # Versioned project manifest
  go.mod                # One module for services and shared Go packages
  .dockerignore         # Root build-context exclusions
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

New projects use `schemaVersion: 1` in `quikdb.yaml`. The native project contract validates
service names, confined source/build/Dockerfile paths, unique ports and routes, environment
variable names, dependency references and routing parity before `dev` starts a process. One root
Go module lets generated services import `shared/logging`, `shared/auth` and `shared/db` without
copying those packages into each service. Service Dockerfiles use the declared root context and
copy only shared code plus that service into the build stage. `add` writes the new service back to
the same manifest. The public JSON Schema is
[`contracts/frame-project-manifest-v1.schema.json`](contracts/frame-project-manifest-v1.schema.json).
Older generated manifests without `schemaVersion` load as the v1 shape and are upgraded when
`add` next writes them. Earlier v1 manifests without build fields load with their service path as
the context. Root-context submission, native workers and edge routing remain backend work; the CLI
fails closed instead of silently building with the wrong context.

## Principles

1. **Compiled over interpreted.** Static Go binaries. No runtime in production.
2. **Zero dependency bleed.** Nothing ships to production except the binary.
3. **Start simple, split when ready.** Begin with `api` + `web`. Split when you need to.
4. **Stateless services.** State lives in the database. Containers restart anytime.
5. **AI-assisted, not AI-dependent.** Works with Claude Code, Cursor, Copilot, or no AI at all.
6. **Deploy-ready from scaffold.** Fresh projects deploy without editing a single file.
7. **Production-first.** Auth, health checks, graceful shutdown, logging, rate limiting from the first commit.

## Convert Existing Apps

Conversion is always opt-in. The first pilot supports one fail-closed subset: Express 4.21.2 on
Node 20 with a single CommonJS entrypoint, fixed routes, constant JSON/string responses and
bounded static assets. It generates real response behavior, never handler stubs. Dynamic or
ambiguous applications are rejected and continue through the existing as-is deployment path.

```bash
quikdb-frame convert ./my-express-app --from express --json
quikdb-frame convert ./my-express-app --from express --output ./my-express-app-frame --apply
quikdb-frame deploy --source ./my-express-app --mode frame --from express --dry-run --json
quikdb-frame deploy --source ./my-express-app --mode frame --from express --name my-app-frame
```

Planning is the default and writes nothing. `--apply` is required to create a native Go/scratch
Frame project. The integrated deploy command also requires the explicit `--mode frame` choice,
materializes its candidate in a private temporary workspace and removes that workspace afterward.
Its review includes deterministic source identity, qualification result and the original Node
settings needed for an as-is fallback. The original source is never modified. Production upload
still requires the server to advertise the complete archive execution capability.

See the [Express conversion pilot](docs/EXPRESS_CONVERSION_PILOT.md) for the exact support matrix,
rejection rules and hosted original-versus-converted response oracle. Flask and all other
frameworks remain as-is only; the broader converter list in SPEC.md is a target, not shipped support.

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

### Local application packaging — v0.1.16

Keep your existing language/framework and provide its original production settings:

```sh
quikdb-frame deploy --source ./my-app --config ./my-app/quikdb.json --name my-app --mode as-is --dry-run --json
quikdb-frame deploy --source ./my-app --config ./my-app/quikdb.json --name my-app --mode as-is
```

The dry run is offline and available in v0.1.16. Packaging excludes common credential locations, local `.env` files and
dependency caches; compiled application output is retained. Review source for other secrets
and configure runtime values separately. Archives are limited to 64 MiB compressed.
Same-source deployment retains application identity; changing an existing application's
archive is pending qualified update fencing. The non-dry-run command performs authenticated
server capability preflight before any upload and currently refuses production submission while
the API and runner flags are off. Use Git deployment for a live as-is application today.

The bounded Express pilot is also available for offline qualification:

```sh
quikdb-frame deploy --source ./my-express-app --name my-express-frame --mode frame --from express --dry-run --json
```

It accepts only Express 4.21.2 on Node 20 with fixed literal responses and bounded static mounts.
Unsupported or ambiguous code fails closed and never changes the user-selected as-is source.
See [v0.1.16 release notes](docs/RELEASE_V0.1.16.md) and the
[conversion support matrix](docs/EXPRESS_CONVERSION_PILOT.md).
