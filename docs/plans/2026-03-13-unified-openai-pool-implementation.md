# Unified OpenAI Pool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make single-account OpenAI configs behave the same as multi-account configs by always routing through the account pool, and switch quota probing/parsing to a Codex-style snapshot flow compatible with the original project.

**Architecture:** Normalize all OpenAI runtime behavior through `EffectiveUpstreams()` and `OpenAIAccountPool`, even when only one account exists. Replace the ad-hoc quota probe/header parsing with a primary/secondary Codex snapshot parser that can normalize to `5h/7d` values for persistence, status output, and probe fallback.

**Tech Stack:** Go, Cobra, existing `internal/upstream` pool/client, runtime JSON persistence, existing OpenAI OAuth/Codex helpers.

---

### Task 1: Add failing tests for unified single-account pool behavior

**Files:**
- Modify: `internal/http/handler/openai_handler_test.go`
- Modify: `cmd/server/main_test.go`
- Test: `internal/http/handler/openai_handler_test.go`
- Test: `cmd/server/main_test.go`

**Step 1: Write the failing tests**

Cover:
- legacy single-account OAuth config still creates a pool-backed path
- single-account mode gets local `/v1/models` behavior and pooled request logging
- `accounts status` for legacy config uses the same pool/probe flow as multi-account

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/handler ./cmd/server -run 'Test(Legacy|ResolveAccountsStatus)'`

Expected: legacy single-account path still bypasses the pool.

**Step 3: Write minimal implementation**

Remove divergent request handling so pool is always the primary path for OpenAI.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/handler ./cmd/server -run 'Test(Legacy|ResolveAccountsStatus)'`

Expected: PASS.

### Task 2: Add failing tests for Codex primary/secondary snapshot parsing

**Files:**
- Create: `internal/upstream/codex_snapshot_test.go`
- Modify: `internal/upstream/account_status_store.go`
- Test: `internal/upstream/codex_snapshot_test.go`

**Step 1: Write the failing tests**

Cover:
- parsing `x-codex-primary-*` and `x-codex-secondary-*` headers
- normalization from primary/secondary to canonical `5h/7d`
- compatibility with existing `X-Codex-5h-*`/`X-Codex-7d-*` headers
- rate-limited probe responses with useful headers still populating status

**Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream -run 'Test(ParseCodexSnapshot|NormalizeCodexSnapshot)'`

Expected: missing snapshot parser/normalizer.

**Step 3: Write minimal implementation**

Add a single snapshot parser and reuse it for probe and response updates.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/upstream -run 'Test(ParseCodexSnapshot|NormalizeCodexSnapshot)'`

Expected: PASS.

### Task 3: Add failing tests for original-project-style OAuth probing

**Files:**
- Modify: `internal/upstream/account_probe_test.go`
- Modify: `internal/upstream/account_status_store.go`
- Test: `internal/upstream/account_probe_test.go`

**Step 1: Write the failing tests**

Cover:
- OAuth probe sends `Version: 0.104.0`
- OAuth probe uses normalized Codex request payload
- OAuth probe accepts primary/secondary quota headers from 429 responses

**Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream -run 'TestProbeAccounts'`

Expected: missing header/version/parser compatibility.

**Step 3: Write minimal implementation**

Update probe request construction and snapshot parsing.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/upstream -run 'TestProbeAccounts'`

Expected: PASS.

### Task 4: Implement unified runtime wiring

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/http/handler/openai_handler.go`
- Modify: `internal/upstream/account_pool.go`

**Step 1: Write/adjust failing tests if needed**

Cover:
- serve path prefers pool for all OpenAI configs
- legacy OAuth no longer depends on separate handler branches

**Step 2: Run tests to verify failures**

Run: `go test ./internal/http/handler ./cmd/server`

Expected: failures until routing is unified.

**Step 3: Write minimal implementation**

Always construct and inject the account pool, and use it consistently for OpenAI request forwarding and status operations.

**Step 4: Run tests to verify it passes**

Run: `go test ./internal/http/handler ./cmd/server`

Expected: PASS.

### Task 5: Verify everything

**Files:**
- Modify: `README.md` if command behavior needs documenting

**Step 1: Run focused suites**

Run:
- `go test ./internal/upstream`
- `go test ./internal/http/handler`
- `go test ./cmd/server`

Expected: PASS.

**Step 2: Run full suite**

Run: `go test ./...`

Expected: PASS.

**Step 3: Commit**

```bash
git add docs/plans/2026-03-13-unified-openai-pool-implementation.md cmd/server/main.go cmd/server/main_test.go internal/http/handler/openai_handler.go internal/http/handler/openai_handler_test.go internal/upstream
git commit -m "refactor: unify openai account routing"
```
