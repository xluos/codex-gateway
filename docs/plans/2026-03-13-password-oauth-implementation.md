# Password OAuth Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the temporary minimal password OAuth implementation with a function-level Go recreation of `perform_codex_oauth_login_http`.

**Architecture:** Introduce a dedicated `CodexPasswordFlow` that mirrors the Python source step-by-step, split sentinel logic into its own module, and keep the existing password token-source and account-pool integration stable on top.

**Tech Stack:** Go, `net/http`, `cookiejar`, existing OAuth flow/store/token source, `httptest`.

---

### Task 1: Add failing branch-parity tests for the flow

**Files:**
- Create: `internal/oauth/codex_password_flow_test.go`
- Modify: `internal/oauth/password_login_test.go`

**Step 1: Write the failing test**

Add tests for:
- OTP branch
- about-you branch
- consent direct redirect branch
- workspace/select then organization/select branch

**Step 2: Run test to verify it fails**

Run: `go test ./internal/oauth -run 'TestCodexPasswordFlow' -v`
Expected: FAIL because the dedicated flow does not exist yet.

**Step 3: Write minimal implementation**

Create a flow type and implement only the pieces required by the first failing test.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/oauth -run 'TestCodexPasswordFlow' -v`
Expected: PASS for the implemented branch.

### Task 2: Move sentinel logic into dedicated files

**Files:**
- Create: `internal/oauth/sentinel.go`
- Create: `internal/oauth/sentinel_test.go`
- Modify: `internal/oauth/password_login.go`

**Step 1: Write the failing test**

Cover:
- requirements token generation
- full sentinel token generation
- large challenge body handling

**Step 2: Run test to verify it fails**

Run: `go test ./internal/oauth -run 'TestSentinel' -v`
Expected: FAIL because the extracted sentinel module does not exist yet.

**Step 3: Write minimal implementation**

Move and adapt the current PoW code into the new sentinel module.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/oauth -run 'TestSentinel' -v`
Expected: PASS.

### Task 3: Replace executor internals with the new flow

**Files:**
- Modify: `internal/oauth/password_login.go`
- Modify: `cmd/server/main.go`
- Modify: `cmd/server/main_test.go`

**Step 1: Write the failing test**

Assert that `HTTPPasswordLoginExecutor` delegates to the stricter flow and still satisfies `password_oauth` pool wiring.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/server -run 'TestNewUpstreamAccountPool_PasswordOAuth'`
Expected: FAIL if the new flow is not wired through the executor path.

**Step 3: Write minimal implementation**

Switch the executor internals to use `CodexPasswordFlow`.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/server -run 'TestNewUpstreamAccountPool_PasswordOAuth'`
Expected: PASS.

### Task 4: Keep token-source behavior green

**Files:**
- Modify: `internal/oauth/password_source_test.go`
- Modify: `internal/oauth/password_source.go`

**Step 1: Write the failing test**

Ensure cached credentials, refresh fallback, and password login fallback still behave the same after the executor rewrite.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/oauth -run 'TestPasswordTokenSource' -v`
Expected: FAIL if the executor rewrite breaks token-source assumptions.

**Step 3: Write minimal implementation**

Adapt only the pieces needed to preserve token-source behavior.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/oauth -run 'TestPasswordTokenSource' -v`
Expected: PASS.

### Task 5: Full verification and live diagnostic run

**Files:**
- Modify: `/Users/bytedance/.codex-gateway/config.duckmail-multi.yaml` only if required by the flow

**Step 1: Run focused tests**

Run:
- `go test ./internal/oauth`
- `go test ./cmd/server`

Expected: PASS.

**Step 2: Run full suite**

Run: `go test ./...`

Expected: PASS.

**Step 3: Run live diagnostic**

Run:
- `go run ./cmd/server accounts status --config /Users/bytedance/.codex-gateway/config.duckmail-multi.yaml --json`

Expected: The command surfaces the exact upstream step reached by the strict flow. Success is ideal, but if Cloudflare still blocks the request, the error must identify the real blocked step from the strict recreation path.
