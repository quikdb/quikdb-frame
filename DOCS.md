# quikdb-frame

Last updated: 2026-09-14

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
rotating scoped sessions. The as-is Git deployment candidate on feat/frame-as-is-deployment
removes the Frame-layout requirement, reuses Compute root detection and accepts explicit
monorepo service configuration. It is being validated and is not released yet. CLI build uses Go 1.27.1;
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
