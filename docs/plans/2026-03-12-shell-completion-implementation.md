# Shell Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `completion` command support for `zsh`, `bash`, and `fish`, and wire idempotent completion installation into `scripts/install.sh`.

**Architecture:** Use Cobra's built-in shell completion generators from the root command so the completion tree stays in sync with CLI definitions. Extend the install script to generate completion assets into `~/.codex-gateway/completions` and source them from the active shell rc file through a marker-guarded block.

**Tech Stack:** Go, Cobra, Bash installer script, Go test, shell smoke assertions

---

### Task 1: Document the design

**Files:**
- Create: `docs/plans/2026-03-12-shell-completion-design.md`
- Create: `docs/plans/2026-03-12-shell-completion-implementation.md`

**Step 1: Write the design doc**

Describe CLI generation, installer integration, shell coverage, idempotency, and testing.

**Step 2: Commit plan context**

Keep the docs in-repo so future changes have a reference point.

### Task 2: Add failing Go tests for the CLI completion command

**Files:**
- Modify: `cmd/server/main_test.go`

**Step 1: Write the failing test**

Add a test that invokes `run([]string{"completion", "zsh"})` and asserts output contains completion script markers and known commands like `start` or `doctor`.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/server -run TestRunCompletionCommandOutputsZshScript -count=1`

Expected: FAIL because the `completion` subcommand does not exist yet.

**Step 3: Write minimal implementation**

Add the `completion` command to the Cobra root command and route to Cobra's generator methods.

**Step 4: Run test to verify it passes**

Run the same command and expect PASS.

### Task 3: Add failing tests for install idempotency

**Files:**
- Create: `scripts/install_test.sh`

**Step 1: Write the failing test**

Create a shell test that:
- builds/installs into a temp HOME / install root
- runs `scripts/install.sh` twice for each supported shell setup
- asserts the rc file contains one completion marker block
- asserts the completion file exists in the expected directory

**Step 2: Run test to verify it fails**

Run: `bash scripts/install_test.sh`

Expected: FAIL because completion files and marker block are not installed yet.

**Step 3: Write minimal implementation**

Update `scripts/install.sh` to generate completion files and append a single marker-guarded source block.

**Step 4: Run test to verify it passes**

Run the same shell test and expect PASS.

### Task 4: Verify no regressions

**Files:**
- Modify: `scripts/install.sh`
- Modify: `cmd/server/main.go`

**Step 1: Run focused verification**

Run:
- `go test ./cmd/server -run TestRunCompletionCommandOutputsZshScript -count=1`
- `bash scripts/install_test.sh`

**Step 2: Run full verification**

Run: `go test ./...`

Expected: all pass.
