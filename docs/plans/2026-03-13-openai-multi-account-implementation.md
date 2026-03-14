# OpenAI Multi-Account Failover Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an OpenAI multi-account pool with failover, account-level model mapping/default models, and a CLI command to inspect quota-related account status.

**Architecture:** Keep a small in-memory account pool that wraps multiple upstream clients. The handler selects an account per request, rewrites the request model according to that account’s mapping/default, retries on quota/rate-limit failures, and updates account status from response headers. CLI reads the same in-memory pool model from config and prints quota status hints.

**Tech Stack:** Go, Cobra, in-memory runtime state, existing upstream proxy code, Go tests.

---

### Task 1: Extend config to support multiple upstream accounts

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add tests that verify:
- `upstreams` can define multiple accounts
- legacy `upstream` still works
- at least one effective upstream account exists
- per-account `default_model` and `model_mapping` load correctly

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run 'TestLoadConfig_(LoadsMultipleUpstreams|LegacyUpstreamStillWorks)'`
Expected: FAIL because `upstreams` is not implemented yet.

**Step 3: Write minimal implementation**

Add config types and validation so the effective account list can be derived from either `upstreams` or legacy `upstream`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config -run 'TestLoadConfig_(LoadsMultipleUpstreams|LegacyUpstreamStillWorks)'`
Expected: PASS

### Task 2: Add an OpenAI account pool and model selection logic

**Files:**
- Create: `internal/upstream/account_pool.go`
- Create: `internal/upstream/account_pool_test.go`
- Modify: `internal/upstream/client.go`

**Step 1: Write the failing test**

Add tests that verify:
- requested models pick explicitly supporting accounts first
- accounts without `model_mapping` act as general-purpose accounts
- requests without model use the selected account’s `default_model`
- equal-priority accounts round-robin
- cooldown excludes an account

**Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream -run 'TestAccountPool_'`
Expected: FAIL because the pool does not exist yet.

**Step 3: Write minimal implementation**

Implement an in-memory pool that builds one upstream client per account and provides:
- selection
- model resolution
- cooldown marking
- snapshot/status reporting

**Step 4: Run test to verify it passes**

Run: `go test ./internal/upstream -run 'TestAccountPool_'`
Expected: PASS

### Task 3: Add handler failover and snapshot updates

**Files:**
- Modify: `internal/http/handler/openai_handler.go`
- Modify: `internal/http/handler/openai_handler_test.go`

**Step 1: Write the failing test**

Add tests that verify:
- `chat/completions` fail over to a second account on quota-limited upstream error
- model mapping/default model rewriting works during forwarding
- successful responses update account snapshot/status

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/handler -run 'Test(ChatCompletions|Responses)Handler_'`
Expected: FAIL because handler still uses a single client.

**Step 3: Write minimal implementation**

Refactor handler construction so it can use the account pool for OpenAI requests while preserving current OAuth-only local model-list behavior where needed.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/handler -run 'Test(ChatCompletions|Responses)Handler_'`
Expected: PASS

### Task 4: Add `accounts status` CLI command

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `cmd/server/main_test.go`

**Step 1: Write the failing test**

Add a test that verifies `codexgateway accounts status` prints configured account names and quota status fields.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/server -run 'TestRunAccountsStatus'`
Expected: FAIL because the command does not exist yet.

**Step 3: Write minimal implementation**

Add an `accounts` command group with `status`, loading config and printing human-readable account status from the pool.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/server -run 'TestRunAccountsStatus'`
Expected: PASS

### Task 5: Update docs and run full verification

**Files:**
- Modify: `README.md`

**Step 1: Update docs**

Document:
- `upstreams` config
- account-level `default_model` / `model_mapping`
- failover semantics
- `codexgateway accounts status`

**Step 2: Run verification**

Run: `go test ./internal/config ./internal/upstream ./internal/http/handler ./cmd/server`
Expected: PASS

Run: `go test ./...`
Expected: PASS
