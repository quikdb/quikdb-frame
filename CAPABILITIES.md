# Frame capability ledger

Last reviewed: 2026-09-14. SPEC.md remains the target; this ledger records implementation
evidence and release gates. A scaffold, interface or compiling stub does not complete an adapter.
Deployment and conversion scope follows the QuikDB ops execution plan.

| SPEC section | Current source / state | Completion gate |
|---|---|---|
| Principles | Go/scratch templates; full production readiness unqualified | Integration and resource measurements |
| api | scaffold/templates.go REST/health examples | Real application, errors, auth, production integration |
| web | Preact/Vite plus Go static server | Browser/API wiring, SPA routes, no localhost production dependency |
| mobile | No mobile CLI/scaffold | SDK/templates and platform integration |
| ws | scaffold/add.go generated service | Independent duplex/reconnect/backpressure tests |
| worker | scaffold/add.go generated service | Queue consumption, idempotency and failure recovery |
| Project Structure | scaffold/init.go creates directories/files | Coherent shared modules and imports across services |
| quikdb.yaml | V1 typed parser/schema; init/add/dev use validated services, paths, ports, routes and dependencies | Deploy/routing integration and full build-context contract |
| quikdb.json | Deploy reads per-service JSON | Explicit settings precedence and legacy compatibility |
| Dockerfile Conventions | Multi-stage Go/web scratch templates | Non-root, CA/TLS, architecture, image size and SBOM gates |
| JWT Structure | Generated shared/auth template | Issuer/audience/expiry/algorithm/rotation tests |
| Refresh Token | No complete Frame application adapter | Rotation/replay/revocation tests |
| OTP Flow | No complete application provider suite | Provider success/failure/rate-limit integration |
| OAuth Flow | No complete application provider suite | State/PKCE/provider integration |
| Password Auth | No complete application adapter | Password hashing/reset/session protections |
| Guest Auth | No complete application adapter | Scoped guest lifecycle and upgrade |
| API Keys | No scoped CLI/application key system | Scope/rotation/revocation/owner isolation |
| Token Validation | Generated middleware example | Cross-service trust contract and negative fixtures |
| Supported Payment Providers | shared/payments directory only | Each declared provider independently tested |
| Payment Router | Not implemented | Currency/provider selection and explicit failures |
| Webhook Verification | Not implemented | Provider signatures/replay tests |
| Payment Idempotency | Not implemented | Concurrent duplicate delivery oracle |
| Transaction Ledger | Not implemented | Durable reconciliation and audit |
| Subscription Lifecycle | Not implemented in Frame | Lifecycle/provider integration |
| Built-in Database Adapters | shared/db interface/retry helper | Real drivers, transactions, cancellation and pooling |
| Connection Behavior | Retry helper; unqualified | TLS, bounds, failover, saturation and leakage tests |
| Redis | No complete Frame adapter | Authentication/TLS/pooling/failure integration |
| Migrations | No CLI migration implementation | Apply/status/down policy and isolated DB fixtures |
| Caching | Empty shared/cache | TTL/invalidation and failure policy |
| Push Notifications | Empty shared/notify | Each declared provider integration |
| Email | Empty shared/notify | Each declared provider integration |
| SMS | Empty shared/notify | Each declared provider integration |
| Structured Logging | Generated logging example | Structured schema and secret-redaction fixtures |
| Request ID Propagation | Partial generated middleware | Multi-service propagation fixtures |
| Health Checks | Generated GET /health | Liveness/readiness/dependency semantics |
| Graceful Shutdown | Generated server shutdown | In-flight requests and worker draining tests |
| Rate Limiting | Config/template aspirations | Distributed limits and failure policy |
| Circuit Breaker | Not implemented | Independent fault/recovery fixtures |
| Distributed Locks | Not implemented | Lease/fencing/concurrent-owner fixtures |
| Optimistic Locking | Not implemented | Conflict/retry/transaction fixtures |
| CLI Installation | v0.1.12 public installer; signed/checksummed binaries; actual Linux/macOS/Windows upgrade passed | Remaining install paths and automatic in-CLI provenance |
| CLI Project Commands | init/add/dev share project manifest v1; no true source watching | Reload, env loading, dependency startup/health and child cancellation |
| CLI Deploy Commands | v0.1.11 as-is Git/root detection/explicit monorepo; actual Node fixture + same-ID resume passed | Local/private/commit/lifecycle/management parity |
| CLI Convert Commands | Express/Flask handler stubs | Optional bounded conversion with independent business-logic tests |
| CLI Generate Commands | Not implemented | Resource/endpoint/provider generation fixtures |
| AI Integration | Generated assistant instruction files | Correct manifest/service guidance and consent for any remote conversion |
| Converter Process/Mapping | Regex scanning and Go stubs | Syntax/semantic IR, adapters, unsupported-code rejection, source oracle |
| Supported Source Frameworks | No preservation-certified framework | Per-version support matrix and independent original/target certification |
| Size Targets | Targets in SPEC; no certified app benchmarks | Publish image/binary/RSS/startup/latency and total cost measurements |
| Distribution & Awareness | Public installer/release/README and Compute CLI entry | Full onboarding/source/settings parity |
| What Frame Is / Is Not | Product positioning | Public claims consistent with this ledger and tested support matrix |

## Independent safety gates

Existing app deployment must preserve source and the user's selected language/framework.
Conversion must never silently replace omitted code with successful placeholder handlers.
Stateful applications require separately qualified durable storage; scratch containers and
community runner disk are not managed databases. Database qualification follows the ops DB plan.
