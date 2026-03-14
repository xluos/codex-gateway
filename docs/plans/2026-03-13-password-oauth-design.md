# Password OAuth Design

## Goal

Allow an OpenAI upstream account to auto-login with `email + password`, then persist standard OAuth credentials to the existing credentials file format so the rest of the gateway continues to operate on normal OAuth tokens.

## Scope

This design now targets a function-level Go recreation of `perform_codex_oauth_login_http` from `protocol_keygen.py`. The goal is no longer a minimal compatible path. The request sequence, headers, cookies, sentinel flow, and consent branches should be materially equivalent.

Included:
- `GET /oauth/authorize`
- `POST /api/accounts/authorize/continue`
- `POST /api/accounts/password/verify`
- `email_otp_verification` handling
- `about-you` / `create_account` handling
- consent redirect handling
- `workspace/select`
- `organization/select`
- `/oauth/token` exchange

Excluded:
- account registration flow
- browser fallback flow
- unrelated CPA upload logic

## Recommended Approach

Replace the current minimal `HTTPPasswordLoginExecutor` internals with a dedicated Go flow that mirrors the Python function structure one step at a time. The executor remains the public entrypoint, but the protocol logic moves into a `CodexPasswordFlow`.

At runtime:
- if the credentials file exists and is still valid, use it
- if it is expired, attempt refresh with the existing refresh-token flow
- if refresh fails or credentials are missing, run the strict Go recreation of the password OAuth flow and save a standard credentials file

This keeps:
- account-pool behavior unified
- single-account and multi-account behavior identical
- password login isolated behind the existing token-source interface

## Architecture

### `CodexPasswordFlow`

Add a new internal flow object, likely in `internal/oauth/codex_password_flow.go`, with methods that map cleanly to the Python function:

- `Authorize`
- `AuthorizeContinue`
- `PasswordVerify`
- `HandleEmailOTP`
- `HandleAboutYou`
- `ResolveConsentCode`
- `ExchangeCode`

Each method should keep request construction close to the Python source so diffing and debugging stay mechanical.

### Supporting Components

- `internal/oauth/sentinel.go`
  - move sentinel challenge fetching and PoW generation here
  - keep inputs and output structure aligned with the Python script
- `internal/oauth/password_login.go`
  - keep only executor-facing wiring and request object adaptation
- existing `PasswordTokenSource`
  - unchanged behavior, different executor implementation

## Behavioral Parity Requirements

The Go flow should match the Python source on these dimensions:

- header templates:
  - `COMMON_HEADERS`
  - `NAVIGATE_HEADERS`
  - Datadog tracing headers
- cookie behavior:
  - persistent jar across all steps
  - `oai-did` set on auth domains
  - reuse of `login_session` and `oai-client-auth-session`
- sentinel behavior:
  - `fetch_sentinel_challenge`
  - `build_sentinel_token`
  - PoW generation inputs and token JSON fields
- branch order:
  - OTP branch before consent resolution
  - about-you branch after OTP validation when needed
  - consent page direct redirect handling
  - workspace/org selection fallback path

The only intentional difference should be output style:
- Python prints status lines
- Go returns structured errors and uses optional debug logging instead of script-style stdout

## Error Handling

Return explicit errors for:
- unsupported or failed OTP retrieval
- missing `continue_url`
- missing consent `code`
- sentinel challenge or PoW failure
- non-2xx OpenAI responses
- Cloudflare challenge HTML responses

Errors should preserve the step name so `accounts status` and logs make it obvious which phase failed.

## Testing

### Offline parity tests

Use `httptest` to simulate:
- normal login
- large sentinel responses
- OTP branch
- about-you branch
- consent direct 302
- workspace/select + organization/select path

Validate:
- request path
- request body
- required headers
- cookie reuse

### Live diagnostic test

Keep the real `accounts status` run only as an integration signal. It is useful for identifying the exact upstream step blocked by Cloudflare, but it is not the source of truth for protocol correctness.

## Files

- `internal/oauth/codex_password_flow.go`
- `internal/oauth/codex_password_flow_test.go`
- `internal/oauth/sentinel.go`
- `internal/oauth/sentinel_test.go`
- `internal/oauth/password_login.go`
- `internal/oauth/password_login_test.go`
- `internal/oauth/password_source.go`
- `internal/oauth/password_source_test.go`
- `cmd/server/main.go`
