# OpenAI Multi-Account Failover Design

## Background

`codex-gateway` currently supports a single upstream OpenAI account. The next step is to support a small local account pool so the gateway can switch accounts when one account hits quota or rate limits, while allowing each account to define its own default model and model mapping.

This design intentionally stays lightweight. It borrows the useful parts of `sub2api`'s OpenAI account management, but avoids database persistence, sticky sessions, or full scheduler complexity.

## Goals

- Support multiple OpenAI upstream accounts in local config.
- Switch to another account when the current account is quota-limited or rate-limited.
- Let each account define:
  - its own `default_model`
  - optional `model_mapping`
- Expose a CLI command to inspect account availability and quota-related status.
- Preserve backward compatibility with the current single `upstream` config.

## Non-Goals

- Multi-platform account pools.
- Persistent account state in a database.
- Sticky sessions.
- Advanced load balancing, weighted scheduling, or concurrency-aware scheduling.
- Guaranteed precise “remaining balance” numbers.

## Reference Behavior From `sub2api`

The original `sub2api` project does not rely on a stable “exact balance” API for OpenAI accounts. Instead, it primarily uses:

1. Upstream response headers and usage snapshots.
2. Temporary unschedulable / rate-limited account state.
3. Failover loops that exclude the failed account and retry another candidate.

The most relevant behaviors to carry over are:

- account-level model routing
- failover on quota / rate-limit errors
- rate-limit reset timestamps as a scheduling input
- status reporting based on usage snapshots rather than a promised exact wallet balance

## Configuration Design

The config gains a new `upstreams` list while preserving the existing `upstream` block.

- If `upstreams` is non-empty, use it.
- Otherwise, convert the legacy `upstream` + `oauth` fields into a single pool entry internally.

Each account entry includes:

- `name`
- `mode`
- `base_url`
- `api_key`
- `oauth.credentials_file`
- `priority`
- `default_model`
- `model_mapping`
- `cooldown_seconds`

Accounts without `model_mapping` are treated as general-purpose accounts that can accept any requested model. If the request omits a model, the selected account’s `default_model` is used.

## Runtime Model

Add an in-memory `OpenAIAccountPool` that owns:

- the configured accounts
- one upstream client per account
- a simple round-robin cursor per priority bucket
- runtime account state:
  - `cooldown_until`
  - `last_error`
  - `snapshot_updated_at`
  - `codex_5h_used_percent`
  - `codex_5h_reset_at`
  - `codex_7d_used_percent`
  - `codex_7d_reset_at`

The pool is process-local and rebuilt on startup.

## Request Flow

For `chat/completions` and `responses`:

1. Parse the request body and extract the requested model.
2. Ask the pool to select an account.
3. Resolve the upstream model:
   - explicit `model_mapping` match wins
   - otherwise keep the requested model
   - if the request model is empty, use `default_model`
4. Forward the request through that account’s upstream client.
5. If the response succeeds:
   - update the account’s quota snapshot from response headers
   - return the response
6. If the response is a failover-eligible upstream error:
   - mark the account on cooldown
   - exclude it from this request
   - retry the next account
7. If no accounts remain:
   - return the last upstream error, or a synthetic “no available accounts”

`GET /v1/models` stays simple:

- For OAuth-only behavior, local model listing can stay as-is.
- For multi-account mode, return a merged local model list derived from configured mappings and defaults rather than proxying a single upstream.

## Failover Error Policy

Initial failover-eligible cases:

- `429` with error text containing `insufficient_quota`
- `429` with error text containing `rate_limit`
- `400`, `403`, or `429` with error text containing:
  - `billing_hard_limit`
  - `quota exceeded`

Non-failover cases:

- invalid request body
- invalid model name / unsupported request parameters
- local auth failures
- other obvious client-side 4xx errors

This keeps switching behavior predictable and conservative.

## Account Selection

Selection rules:

1. Exclude accounts already tried in this request.
2. Exclude accounts still within cooldown.
3. Prefer accounts that explicitly support the requested model.
4. Include general-purpose accounts with no `model_mapping`.
5. Prefer lower `priority`.
6. Within equal priority, use round-robin.

No sticky session or concurrency state is introduced.

## Quota Status Command

Add:

```bash
codexgateway accounts status
```

This command returns status derived from runtime signals, not a guaranteed precise balance. For each account, report:

- name
- mode
- priority
- default model
- current status: `available`, `cooldown`, `rate_limited`, `unknown`
- cooldown expiry, if any
- latest usage snapshot time
- 5h usage percent and reset time
- 7d usage percent and reset time
- latest error summary, if any

This mirrors the original project’s practical “availability + quota hint” approach.

## Testing Strategy

Write tests first for:

- config compatibility with `upstream` and `upstreams`
- account selection and model resolution
- failover behavior on quota-limited responses
- snapshot extraction into account status
- CLI output for `accounts status`

## Recommended Scope

This first version should stay minimal and avoid:

- periodic background probing
- persistent rate-limit state across restarts
- complex weighted scheduling
- admin APIs

The goal is a reliable local failover pool, not a full account orchestration platform.
