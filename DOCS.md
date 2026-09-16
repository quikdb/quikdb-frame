# quikdb-frame

Last updated: 2026-09-16

The local archive client now requires the complete server-advertised v1 capability before any
upload. It sends the packaged SHA-256 identity, retries one transient upload failure against the
idempotent server contract, verifies the returned handle/digest/size, and releases an unused handle
when deployment creation is rejected. Production activation and a CLI release remain separate gates.

## Native shared module and build context — stacked feature branch

`internal/project` is the typed source of truth for native `quikdb.yaml`. In addition to validated
service paths, ports and types, v1 now declares one `goModule` and each service's repository-relative
build context, Dockerfile and optional target. Dockerfiles must stay inside their declared context.
The loader upgrades the earlier v1/unversioned shape in memory by treating each old service path as
its context and its local Dockerfile as the build file.

New projects have one root `go.mod`; nested service modules are no longer generated. API, web and
added Go services import the shared logging package. API scaffolds also use shared auth middleware
and the database status contract, while workers use logging and database status. Dockerfiles build
from the repository root, copy only the selected service plus shared Go source into the builder,
and copy only the executable/assets into the final image. Generated application logs use
application fields and do not name the hosting provider or runner.

Native deployment now discovers services from `quikdb.yaml`, not directory names or per-service
JSON, and carries the declared path, port and type. The deployed API/runner currently use one
`subdirectory` as both service root, Docker context and Dockerfile directory. A root-context Frame
service therefore fails before authentication or submission with a generic capability error;
workers also fail closed because the current deployment contract requires an HTTP port. Legacy
same-context HTTP services remain representable. The feature branch passed remote race tests, vet,
schema/security/platform checks, generated-service builds and runtime/load/container validation;
it remains unreleased pending review and merge.

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
keyring v0.2.6 and flock v0.13.0 are locked in go.mod/go.sum. Historical Express/Flask handler
stub generation was removed on the conversion-pilot branch. Core adapters and production API/web wiring need
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

## Express static-response conversion pilot — draft

`express-static-v1` replaces the old route-stub path with one bounded converter. Planning is
side-effect-free by default and `--apply` is explicit. The accepted matrix is exact Express
4.21.2 on Node 20, one CommonJS entrypoint, fixed literal routes/responses and bounded static
mounts. Unknown statements, request-dependent logic, extra server source/dependencies,
middleware, state and other frameworks fail closed with the as-is path retained.

Applied output is atomic and deterministic: a native Go/scratch Frame service, source-hashed
review plan, names-only environment template and original Node startup/deployment manifest.
The original source is not modified. Hosted qualification compares original and converted status,
content type and body bytes for the fixture oracles, asset bytes, deterministic output, rejection
fixtures, generated builds and container image size. This branch is unreleased and does not enable
deployment-time conversion in the API/dashboard. See docs/EXPRESS_CONVERSION_PILOT.md.

## Local archive uploads — feature branch, not released

2026-09-15: feat/frame-archive-uploads adds deploy --source <directory> --config quikdb.json
--name <application> --mode as-is. Offline --dry-run --json packages/validates without auth or
network. Regular files/directories only; rooted reads reject escaping paths/links/special files,
compressed64MiB/expanded1GiB/file256MiB/20000 entries. Common credential locations/.env files
and dependency caches are excluded; dist/build output and executable bits retained. Exclusions
are not a general secret scanner. Runtime values remain explicitly configured separately.

Account preflight precedes uploads. Exact SHA256/size/opaque UUID receipt checked; current
credential loaded and mutations never automatically retried. Create sends sourceId, never
Git branch metadata. Repeat same archive observes/resumes the existing ID; mismatched archive/
Git source fails without upload or a duplicate app. Changed-archive replacement is pending
safely fenced updates, explicitly not silently reusing old code. Requires activated immutable
API consumer/compatible application runners; no production activation or version tag yet.
Remote CI/native/three-platform gates required before release.
