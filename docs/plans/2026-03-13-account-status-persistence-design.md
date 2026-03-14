# Account Status Persistence Design

## Goal

Make `codexgateway accounts status` show useful OpenAI quota data in two cases:

1. When the gateway service is running, reuse the latest in-memory quota snapshot by persisting it to the runtime directory.
2. When the gateway service is not running, or a specific account has no snapshot yet, issue one minimal probe request for that account and display the result.

## Current Problem

`accounts status` currently creates a fresh account pool in a new CLI process and prints that new pool's empty in-memory state. The live service instance updates snapshots in memory only, so the CLI cannot see them.

## Options Considered

### Option 1: Runtime persistence plus probe fallback

Persist account status snapshots under `runtime.dir`, then let the CLI read that file and probe only the missing accounts.

Pros:
- Fast when the service is running.
- Works even when the service is stopped.
- Keeps runtime state local and simple.

Cons:
- Adds a small persistence layer.
- `accounts status` can become slower if it needs live probes.

### Option 2: Probe every account on every status call

Always fetch fresh data with online requests.

Pros:
- No persistence code.

Cons:
- Slower every time.
- Consumes extra upstream calls even when recent data already exists.

### Option 3: Query a local service endpoint

Expose service memory through a local API and let the CLI fetch it.

Pros:
- Fresh while service is running.

Cons:
- Fails when service is stopped.
- More moving parts than needed.

## Recommendation

Use Option 1.

It matches the current single-process runtime model, avoids a new internal API, and still gives the user usable data when the server is down.

## Design

### Runtime snapshot file

Add a second runtime JSON file in `runtime.dir`, named `accounts-status.json`.

This file stores:
- `updated_at`
- account status entries keyed by account name
- quota snapshot fields already exposed by `upstream.AccountStatus`

The format should stay append-free and be rewritten atomically, the same way the existing runtime state file is written.

### Service-side persistence

When the request handler receives a successful upstream response and updates the account pool snapshot, it should also persist the pool's current `AccountsStatus()` view to `accounts-status.json`.

This is best-effort:
- request serving must not fail because snapshot persistence fails
- persistence failures should be logged

### CLI loading flow

`codexgateway accounts status` should:

1. Load config.
2. Build the account pool.
3. Read `accounts-status.json` if it exists.
4. Merge persisted entries into the fresh pool for matching account names.
5. Identify accounts missing a useful snapshot.
6. Probe only those accounts.
7. Merge probe results back into the pool and rewrite `accounts-status.json`.
8. Render text output or JSON output from the merged result.

### Probe behavior

Probe only when an account lacks both `5h` and `7d` snapshot data.

The probe request should:
- use the account's configured client
- use the smallest safe request body we can rely on
- prefer the account's `default_model`
- fall back to a conservative default model when needed
- collect headers even on eligible non-2xx responses like `429`, because Codex quota headers can still be useful there

Probe failures should:
- set `last_error`
- keep the account visible in output
- not abort the whole command if other accounts can still be shown

### JSON output

`--json` should continue to emit the same machine-readable fields as today, but from the merged persisted-plus-probed status result.

### Text output

The current Unicode bar display stays unchanged, but now its values come from persisted snapshots first, then probe fallback.

## Testing

Add tests for:
- runtime snapshot file write and read
- persisted snapshot merge into a new pool
- CLI status reading persisted data without probing
- CLI probing only missing accounts
- probe failure preserving output with `last_error`
- handler persistence hook being best-effort
