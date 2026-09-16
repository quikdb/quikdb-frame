# quikdb-frame

Last updated: 2026-09-16

## Native project manifest v1 — feature branch

`internal/project` is the typed source of truth for native `quikdb.yaml`. It strictly parses one
bounded regular YAML file, accepts the previous unversioned generated shape as v1, rejects unknown
fields and validates project/service names, confined paths, service types, unique ports/routes,
environment names, dependency references/cycles and exact routing parity. The public JSON Schema
and contract cases live under `contracts/frame-project-manifest-v1.*`.

`init` and the Express/Flask migration scaffold serialize through this contract, removing the
malformed same-line routing maps. `add` validates its input, writes generated services into the
manifest and rolls back the new service directory if the manifest cannot be saved. `dev` now uses
declared service paths, types and ports and returns when every child exits instead of waiting
forever. Generated API/WS/worker/web `quikdb.json` files are parsed in remote regression tests.

This slice does not wire native deployment/edge routing, shared Go modules, hot reload, production
API/browser wiring, immutable artifacts or conversion semantics. It is unreleased until its branch
passes remote CI and is reviewed and merged.

Released v0.1.13 at 4045ff9: feat/frame-cli-management adds ID-based status/inspect/logs/
history/lifecycle/config/resources/environment/domain management through dashboard APIs.
See docs/CLI_MANAGEMENT.md. Safe public detail excludes env values/bundled logs; env set reads
explicit file/stdin and redacts values, export creates a new private POSIX/Windows file.
API ownership/scope/resource and portal consent prerequisites deployed before release. Human CLI
management is not delegated agent/database access. Full source/artifact/rollback fencing pending.

Developer CLI and Go/Preact service scaffolding. The separate quikdb-cli-go binary is the
node/operator runner. Frame releases are distributed through GitHub Releases, not EKS.

## Deployment contract

- internal/deploy/client.go uses the existing authenticated Compute deployment API.
  List reads data.deployments/pagination; detail reads data.deployment. Legacy shapes remain
  readable. All pages are fetched, duplicate/incomplete results are errors.
- Requests have a 30-second timeout; waiting is interruptible and bounded to 30 minutes.
  A wait timeout does not cancel the platform deployment. Native credentials refresh before
  each request when needed, so a 15-minute access token does not break a longer build. Failed, partial and stopped states
  produce failures. HTTP/auth/quota/JSON errors never become an empty successful account.
- Deploy lists existing applications first, verifies repository/branch/service-directory
  ownership, and uses the same deployment ID to redeploy failed/stopped/sleeping applications.
  Active builds are observed; live applications retain the existing push-triggered update flow.
- Local .env and .env.example values are never automatically uploaded. Configure runtime
  values explicitly through Compute before deploying applications that require them.
- QUIKDB_TOKEN can directly authenticate commands in CI without persisting credentials.

## Key files and validation

- cmd/quikdb-frame/main.go: command dispatch and nonzero error exit status.
- internal/deploy/client_test.go: API contracts, pagination, terminal states, timeout,
  failed-application identity, collision isolation and explicit configuration regressions.
- .github/workflows/ci.yml: remote regression/vet plus scaffold/build/container checks.
- .github/workflows/release.yml: remote tests/vet, platform builds and SHA256SUMS.
- Frame is public: validation and release use isolated GitHub-hosted Linux runners, without
  access to production EKS credentials. Existing ARC did not acquire its queued job. The API
  remains private and its production deployment uses ARC; its GitHub repo is quikdb-device-apis.
- internal/upgrade/: resolves one immutable release tag, checks the platform artifact against
  SHA256SUMS and retains the installed binary on missing/mismatched checksums or failed download.

## Known gaps

This is not complete Frame or universal host/runtime compatibility. v0.1.10 publishes native
PKCE/device-code approval, protected credential storage, cross-process refresh locking and
rotating scoped sessions. v0.1.11 as-is Git deployment
removes the Frame-layout requirement, reuses Compute root detection and accepts explicit
monorepo service configuration. Its source gates and published Linux/Windows checks passed;
macOS download/provenance passed but self-upgrade hit GitHub anonymous API quotas.
The next patch uses the public latest-release redirect with strict origin/repository/tag validation.
v0.1.12 at 216a052 passed exact-source gates and actual signed downloads/self-upgrades on
Linux x64, macOS ARM64 and Windows x64 (34830166123). Actual Linux installer passed in EKS.
The approved fixture device login, live concurrent refresh, original Node-v22 deployment,
repeat identity and stopped-app resume passed; both replicas live before cleanup. Fixture deleted,
access/refresh revoked (401 verified), credential file cleared and temporary workspace removed.
CLI build uses Go 1.27.1;
keyring v0.2.6 and flock v0.13.0 are locked in go.mod/go.sum. Express/Flask conversion generates
handler stubs and must not be treated as business-logic preservation. Core adapters and production API/web wiring need
the subsequent work packages. No managed databases have been provisioned by this change.

## Recent changes

2026-09-14: deployment contract repair on feat/frame-deployment-contracts. Validation and
release status are tracked in EA/workstreams/active/quikdb-frame-universal-deployment.md.

v0.1.9 published from 31651f6: remote contract/race/vet/platform builds and full scaffold,
Docker/runtime/load checks passed. Published Linux asset download/checksum/version/upgrade
passed in an isolated credential-free remote job. Other platform binaries were built, but
desktop credential integration belongs to the next native-login milestone.

## v0.1.13 management qualification

Full source/native/main gates 34839661303, 34839661301 and 34840141568 passed;
signed release 34840605601 passed. Five binaries/checksums/provenance published.
API e2a7145 ownership/scopes/resources deployed and portal 96d853c exact consent deployed.
Older sessions retain deployment-only access; logout/new login explicitly approves env/domain
access, including secret export. Actual published three-OS checks and fixture acceptance are
recorded in the ops 08-CLI-MANAGEMENT-2026-09-14 report as they complete.

## Source/settings detection — released 2026-09-14

feat/frame-source-settings passes --subdirectory to the same owner-scoped detection API
and rejects a returned directory mismatch. Explicit --config remains authoritative. API must
be deployed before releasing automatic service-root detection. Immutable commit/local-source/
cross-context Dockerfile/target and versioned shared manifest contracts remain subsequent WP2 work.

Source/settings qualification: runtime 6d59d6e passed full 34843649175, native/desktop/security 34843649225 and main 34843947357. API final 40b747e passed 140 tests (34844097886), production 34844237593 healthy at generation 247/two ready replicas. v0.1.14 tagged on that exact CLI source; signed release34844547094 and actual published Linux/macOS/Windows34844788753 passed. Fresh live account fixtures remain separate. README reflects the directory-detection change. No complete WP2 or conversion certification.

## Deployment manifest v1 — v0.1.15 qualified

Canonical contracts/deployment-manifest-v1.schema.json and shared acceptance cases define a single-service as-is quikdb.json, separate from native quikdb.yaml. V1 requires runtime, original install/build/start commands and port; strict unknown fields/versions, no environment values/source/agent grants. Optional framework/health default to empty and /. Node version is major-only (1–99), with cross-field consistency enforced at runtime. Runtime availability remains separately qualified.

CLI manifest validate --file quikdb.json --json runs offline and reports metadata only. --config accepts v1 and legacy explicit Compute objects; v1 normalizes runtime to appType and strips schema metadata before API submission. Explicit --port wins. Invalid/bounded/nonregular files fail before authentication. Shared API corpus and public JSON Schema checks, remote Go/native/desktop gates must pass before release. Immutable sources/full stored precedence/context and conversion remain pending.

Remote qualification: d287a76 full34846859517/native34846859537 passed, public schema39 cases and real macOS/Windows checks passed. API d08f609 shared pinned contracts plus nine suites/186 tests34846944145 passed; production rollout pending. Published-platform workflow can opt into actual offline manifest success/unsupported-version rejection on all three OSes after the tagged release.

Final runtime de867b723832f5f681c5c02c9c6665450094cbd0 passed full/native34847304717/34847304730 and main34847715726. Tagged v0.1.15 signed release34847937129 passed. Actual published Linux/macOS/Windows34848153626 passed checksum/provenance/version/self-upgrade plus offline manifest success and sanitized unsupported-version rejection; manifest input enabled for all jobs. Linux x64 checksum7403e5e5508721bab9cd018e8648219265e0b24ddd5707746f23ff41197f886b. API prerequisite deployed generation248/two ready. Offline checks need no fixture session; fresh authenticated live remains separate.
