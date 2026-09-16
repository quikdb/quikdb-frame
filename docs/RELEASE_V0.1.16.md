# QuikDB Frame v0.1.16

v0.1.16 makes the universal deployment MVP downloadable for local evaluation.

## Available now

- Package an existing local application and inspect its sanitized deployment metadata offline:

  ```sh
  quikdb-frame deploy --source ./my-app --config ./my-app/quikdb.json \
    --name my-app --mode as-is --dry-run --json
  ```

- Qualify the bounded Express subset and inspect the generated Frame plan and as-is rollback
  metadata without changing the original source:

  ```sh
  quikdb-frame deploy --source ./my-express-app --name my-express-frame \
    --mode frame --from express --dry-run --json
  ```

- Explicitly generate a reviewed converted candidate with `convert --apply`. The accepted subset
  is Express 4.21.2 on Node 20, one CommonJS entrypoint, fixed literal responses, and bounded static
  mounts. Unsupported logic fails closed without a partial conversion or automatic fallback.

## Live deployment status

Git-based `--mode as-is` deployment remains the supported live path. Local archive submission and
converted archive submission both require the complete server-advertised archive v1 capability.
Production API and runner flags remain off, so the CLI stops at capability preflight before upload.
This release does not activate source consumption, replace a runner, provision storage, or upload a
fixture application.

## Verification

Release publication requires race tests, vet, cross-platform builds, converter source/target HTTP
and asset parity, converted container size comparison, source-archive safety tests, installer tests,
signed build provenance, and SHA-256 checks. A separate published-assets workflow downloads the
actual Linux, macOS, and Windows binaries and verifies checksum, provenance, version, self-upgrade,
manifest validation, as-is packaging dry-run, and Express conversion dry-run.
