# Build And Install Script Split Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Split local developer build behavior from end-user installation so local rebuilds use `scripts/build.sh` and the repository-root `install.sh` is reserved for remote installation.

**Architecture:** Keep the existing local target path `~/.codex-gateway/bin/codexgateway`, but move that responsibility into a dedicated build script. Replace the old local installer with a compatibility shim, and update docs/tests to point to the new responsibilities.

**Tech Stack:** Bash, Go toolchain, repository docs.

---

### Task 1: Define the new local build behavior in tests

**Files:**
- Modify: `scripts/install_test.sh`

**Step 1: Write the failing test**

Change the shell test so it executes `scripts/build.sh` twice and verifies:
- `~/.codex-gateway/bin/codexgateway` exists
- `~/.codex-gateway/bin/cgw` exists as a symlink
- no shell rc file is created or modified

**Step 2: Run test to verify it fails**

Run: `bash scripts/install_test.sh`
Expected: FAIL because `scripts/build.sh` does not exist yet.

### Task 2: Implement the script split

**Files:**
- Create: `scripts/build.sh`
- Modify: `scripts/install.sh`
- Create: `install.sh`

**Step 1: Write minimal implementation**

Implement `scripts/build.sh` to compile `./cmd/server` into `~/.codex-gateway/bin/codexgateway`, refresh the `cgw` symlink, and verify the binary runs. Reduce `scripts/install.sh` to a compatibility message that points developers to `scripts/build.sh`. Add a repository-root `install.sh` that is clearly reserved for future release-based installation.

**Step 2: Run tests to verify they pass**

Run: `bash scripts/install_test.sh`
Expected: PASS

### Task 3: Update docs and references

**Files:**
- Modify: `README.md`

**Step 1: Update usage docs**

Replace local setup references from `scripts/install.sh` to `scripts/build.sh`, describe the root `install.sh` as the future remote installer, and keep uninstall guidance accurate.

**Step 2: Verify docs references**

Run: `rg -n "scripts/install\\.sh|scripts/build\\.sh|^./install\\.sh$" README.md scripts docs -S`
Expected: local setup docs use `scripts/build.sh`, and only intentional references to `install.sh` remain.
