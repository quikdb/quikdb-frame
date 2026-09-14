# quikdb-frame

Last updated: 2026-09-14

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
