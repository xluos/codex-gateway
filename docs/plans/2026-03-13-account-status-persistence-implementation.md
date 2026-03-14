# Account Status Persistence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist live OpenAI account quota snapshots to the runtime directory and let `codexgateway accounts status` load them first, then probe only missing accounts.

**Architecture:** Add a small runtime-backed status store, wire the request handler to persist the in-memory pool snapshot after successful updates, and change the CLI to merge persisted state with a fresh pool before optionally probing missing accounts. Keep everything file-based and best-effort.

**Tech Stack:** Go, Cobra, JSON runtime files, existing `internal/upstream` pool/client abstractions.

---

### Task 1: Add failing tests for runtime account status storage

**Files:**
- Create: `internal/upstream/account_status_store_test.go`
- Modify: `internal/upstream/account_pool.go`
- Test: `internal/upstream/account_status_store_test.go`

**Step 1: Write the failing tests**

Cover:
- saving account statuses to `accounts-status.json`
- loading them back
- merging persisted data into a new pool by account name

**Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream -run 'Test(AccountStatusStore|OpenAIAccountPool_ApplyPersistedStatus)'`

Expected: missing store and merge functions.

**Step 3: Write minimal implementation**

Add a store helper and a pool merge method.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/upstream -run 'Test(AccountStatusStore|OpenAIAccountPool_ApplyPersistedStatus)'`

Expected: PASS.

### Task 2: Add failing tests for CLI loading persisted status and skipping probes

**Files:**
- Modify: `cmd/server/main_test.go`
- Modify: `cmd/server/main.go`
- Test: `cmd/server/main_test.go`

**Step 1: Write the failing tests**

Cover:
- `accounts status` rendering persisted snapshot data
- `accounts status --json` using merged results
- no probe call when persisted snapshot already exists

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/server -run 'TestRunAccountsStatus(UsesPersistedSnapshots|SkipsProbeWhenSnapshotExists|JSON)'`

Expected: missing persisted load / probe injection behavior.

**Step 3: Write minimal implementation**

Add CLI-side load/merge logic and test seams.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/server -run 'TestRunAccountsStatus(UsesPersistedSnapshots|SkipsProbeWhenSnapshotExists|JSON)'`

Expected: PASS.

### Task 3: Add failing tests for missing-snapshot probe fallback

**Files:**
- Create: `internal/upstream/account_probe_test.go`
- Modify: `internal/upstream/client.go`
- Modify: `internal/upstream/account_pool.go`
- Test: `internal/upstream/account_probe_test.go`

**Step 1: Write the failing tests**

Cover:
- probe sends a minimal request for accounts without snapshots
- probe accepts useful quota headers even on rate-limited response
- probe sets `last_error` on hard failure

**Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream -run 'TestProbeAccounts'`

Expected: missing probe logic.

**Step 3: Write minimal implementation**

Add a probe helper on the pool or alongside it, using existing clients.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/upstream -run 'TestProbeAccounts'`

Expected: PASS.

### Task 4: Add failing tests for service-side persistence hook

**Files:**
- Modify: `internal/http/handler/openai_handler_test.go`
- Modify: `internal/http/handler/openai_handler.go`
- Test: `internal/http/handler/openai_handler_test.go`

**Step 1: Write the failing tests**

Cover:
- successful upstream response triggers snapshot persistence callback
- persistence failure does not fail the request

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/handler -run 'TestProxyWithAccountPool_(PersistsSnapshot|PersistenceFailureDoesNotFailRequest)'`

Expected: missing persistence hook.

**Step 3: Write minimal implementation**

Add an optional persistence callback to the handler.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/handler -run 'TestProxyWithAccountPool_(PersistsSnapshot|PersistenceFailureDoesNotFailRequest)'`

Expected: PASS.

### Task 5: Wire runtime store into the main command and service startup

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/upstream/account_pool.go`
- Create: `internal/upstream/account_status_store.go`
- Test: `cmd/server/main_test.go`

**Step 1: Write the failing integration-level tests**

Cover:
- main command loads persisted statuses from `runtime.dir/accounts-status.json`
- service startup uses a persistence callback targeting that file

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/server -run 'TestRunAccountsStatus'`

Expected: missing wiring.

**Step 3: Write minimal implementation**

Connect the status store into CLI and handler construction.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/server`

Expected: PASS.

### Task 6: Verify the full suite

**Files:**
- Modify: `README.md` if behavior needs documenting

**Step 1: Run targeted tests**

Run:
- `go test ./internal/upstream`
- `go test ./internal/http/handler`
- `go test ./cmd/server`

Expected: PASS.

**Step 2: Run full test suite**

Run: `go test ./...`

Expected: PASS.

**Step 3: Commit**

```bash
git add docs/plans/2026-03-13-account-status-persistence-design.md docs/plans/2026-03-13-account-status-persistence-implementation.md internal/upstream internal/http/handler cmd/server README.md
git commit -m "feat: persist and probe account status"
```
