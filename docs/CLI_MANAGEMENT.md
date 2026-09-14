# Manage the same applications as Compute

Management commands use the existing Compute APIs and exact `deploymentId` values from
`quikdb-frame status --json`. They do not identify applications by a guessed name. Commands
support `--json` and accept flags before or after IDs. Sign in with the existing CLI flow.

```text
quikdb-frame status --json
quikdb-frame status <id> --json
quikdb-frame inspect <id> --json
quikdb-frame logs <id> --limit 100 --phase runtime
quikdb-frame logs <id> --follow --json
quikdb-frame history <id> --json
quikdb-frame config get <id> --json
quikdb-frame resources get <id> --json
quikdb-frame env list <id> --json
quikdb-frame domains list <id> --json
```

`inspect`, configuration and environment-list results omit environment values. Log messages
are application output and can contain secrets; review/redact them before sharing with chat.
Follow polls bounded recent records for up to 30 minutes, deduplicates stable log IDs and emits
JSON lines with `--json`. A burst larger than the selected limit can omit records; this is not
a lossless archive or a Socket.IO subscription. Ctrl+C stops polling, not the application.

## Explicit settings changes

Create a JSON patch file containing only the fields you intend to change:

```json
{"port":3000,"healthCheck":"/","autoDeployEnabled":false}
```

```text
quikdb-frame config set <id> --file settings.json --json
quikdb-frame resources set <id> --cpu 0.25 --ram 256 --json
```

Configuration accepts `repositoryBranch`, `buildCommand`, `startCommand`, `installCommand`,
`port`, `healthCheck`, `subdomain`, `autoDeployEnabled` and `autoDeployBranch`. Environment
values and unknown fields are rejected. Configuration is saved; commands/port require a
redeploy to apply. Subdomain changes are immediate. Resource changes validate the current
plan and state, and schedule a restart for running applications. Acceptance does not prove
the restart finished: check status and the actual response. Stopped apps remain stopped.

## Environment values

Values are read from an explicit file or stdin, never an inline value argument:

```text
quikdb-frame env set <id> FIXTURE_VALUE --value-file value.txt --json
quikdb-frame env set <id> PUBLIC_LABEL --value-file label.txt --public --json
quikdb-frame env remove <id> FIXTURE_VALUE --json
quikdb-frame env export <id> --output private-app.env --json
```

Set creates or updates the exact key and defaults to encrypted secret storage. `--public`
explicitly stores a nonsecret value. Input bytes, including final newlines, are preserved;
values must be 1–65536 UTF-8 bytes without NUL. Changes need redeployment to take effect.
Environment lists and write results return metadata, not values. Export is explicit plaintext:
it creates a new 0600 file on POSIX or a file with a protected current-user DACL on Windows,
never overwrites an existing file and removes a failed export. Do not commit or share it.
Every backend environment operation checks current application ownership, including dashboard
bulk import/export. The CLI resolves the application document ID from the public deployment ID.

## Lifecycle and domains

```text
quikdb-frame stop <id> --json
quikdb-frame restart <id> --json
quikdb-frame redeploy <id> --json
quikdb-frame wake <id> --json
quikdb-frame rollback <id> --version 0 --json
quikdb-frame domains add <id> app.example.com --json
quikdb-frame domains verify <id> <domain-id> --json
quikdb-frame domains repair <id> <domain-id> --json
quikdb-frame domains remove <id> <domain-id> --json
quikdb-frame delete <id> --yes --json
```

Domain addition is subject to existing plan limits and returns DNS instructions. Connect a
provider in the dashboard first, then optionally supply its `--provider-connection` ID.
The CLI cannot connect/export provider credentials or use customer-agent data endpoints.
Infrastructure/domain/environment management does not grant application-database access.

Rollback uses the existing API convention: `0` is the most recent retained previous version;
timeline rows are not rollback indices. Exact immutable image/source rollback and concurrent
attempt fencing remain the artifact work package; this command does not certify those features.
If a mutation fails or its response is lost, inspect the dashboard/state before retrying.
The client never automatically retries a mutation.

## Disposable acceptance test

Deploy the public QuikDB Node-v22 fixture as-is under a unique test name. Save the returned ID.
Check that inspect/config/resources/history/logs show that same app in the dashboard. Set one
synthetic environment key; confirm it appears masked in Compute, export to a disposable private
file, remove the key and verify it is absent. Change a harmless configuration field and read
it back. Stop and resume the same ID; verify the original HTTP response after resuming. Delete
only the fixture with `--yes`, logout and remove disposable secret/export files. Domain writes
need a disposable domain and an eligible plan; never alter an unrelated production domain.

New source is not a release qualification until the exact revision's remote gates pass. Full
source/settings parity, Frame core, preserved conversion and managed databases remain separate.

## Approve management permissions

Version 0.1.13 requests deployment, environment, and domain permissions during a new login.
The connection page discloses environment secret export before approval. Existing sessions
retain deployment-only permission, including after refresh. To use environment or domain
commands with an older session, run `quikdb-frame logout`, then `quikdb-frame login`
(or `login --device`) and approve the displayed permissions. These human CLI permissions
are separate from the planned agent infrastructure and application-data permissions.
