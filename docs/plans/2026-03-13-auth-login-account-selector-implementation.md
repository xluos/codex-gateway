# Auth Login Account Selector Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `codexgateway auth login` default to an interactive OAuth account selector for multi-account configs, with credential status shown in the list and a confirmation prompt before overwriting a still-valid credential.

**Architecture:** Extend the CLI auth flow to discover OAuth accounts from `EffectiveUpstreams()`, inspect each account's credential file, render a simple numbered selector, and then run the existing login flow against the selected credential path. Preserve a direct non-interactive path through `--account`.

**Tech Stack:** Go, Cobra, existing config/upstream model, existing OAuth store and CLI text output.

---

### Task 1: Add failing tests for account discovery and status rendering

**Files:**
- Modify: `internal/cli/auth_test.go`
- Modify: `internal/cli/auth.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Cover:
- OAuth accounts are discovered from `EffectiveUpstreams()`
- each account exposes `missing`, `expired`, or `valid`
- status output includes default model and credential metadata

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run 'Test(AuthLoginSelector|OAuthAccountStatuses)'`

Expected: missing selector/status helpers.

**Step 3: Write minimal implementation**

Add helper types/functions for account status inspection.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run 'Test(AuthLoginSelector|OAuthAccountStatuses)'`

Expected: PASS.

### Task 2: Add failing tests for interactive selection and overwrite confirmation

**Files:**
- Modify: `internal/cli/auth_test.go`
- Modify: `internal/cli/auth.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Cover:
- `auth login` prompts to select an OAuth account when multiple are configured
- a valid credential requires overwrite confirmation
- declining confirmation cancels login cleanly
- selecting an expired or missing credential proceeds

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run 'TestAuthLogin_(PromptsForAccountSelection|ValidCredentialRequiresConfirmation)'`

Expected: current login flow has no input-driven selection or confirmation.

**Step 3: Write minimal implementation**

Add `stdin`-driven selection and confirmation around the existing login flow.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run 'TestAuthLogin_(PromptsForAccountSelection|ValidCredentialRequiresConfirmation)'`

Expected: PASS.

### Task 3: Add failing tests for `--account`

**Files:**
- Modify: `cmd/server/main_test.go`
- Modify: `cmd/server/main.go`
- Test: `cmd/server/main_test.go`

**Step 1: Write the failing test**

Cover:
- `codexgateway auth login --account backup` passes the selected account to the CLI auth layer

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/server -run 'TestAuthLoginCommand_PassesAccountFlag'`

Expected: missing flag/wiring.

**Step 3: Write minimal implementation**

Add `--account` to the command and pass it into auth login.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/server -run 'TestAuthLoginCommand_PassesAccountFlag'`

Expected: PASS.

### Task 4: Verify the auth flows

**Files:**
- Modify: `README.md` if behavior needs documenting

**Step 1: Run targeted tests**

Run:
- `go test ./internal/cli`
- `go test ./cmd/server`

Expected: PASS.

**Step 2: Run full suite**

Run: `go test ./...`

Expected: PASS.
