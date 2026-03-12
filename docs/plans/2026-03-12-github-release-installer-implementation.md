# GitHub Release Installer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a GitHub Releases-based installer at the repository root and a release workflow that publishes platform-specific tarballs plus checksums.

**Architecture:** The repository-root installer will resolve the current OS and architecture, query GitHub Releases for either `latest` or a specified tag, download the matching tarball and `checksums.txt`, verify SHA-256, and install the binary into `~/.codex-gateway/bin`. A tag-triggered GitHub Actions workflow will build four target archives whose names match the installer's lookup rules.

**Tech Stack:** Bash, Go, GitHub Actions, local shell-script tests.

---

### Task 1: Define installer behavior with a failing shell test

**Files:**
- Create: `scripts/release_install_test.sh`

**Step 1: Write the failing test**

Create a shell test that:
- builds a fake `codexgateway` executable payload
- packages fake release archives for the 4 supported platforms
- serves fake GitHub API responses and assets from a local HTTP server
- runs `./install.sh` against the fake server
- verifies `~/.codex-gateway/bin/codexgateway` and `cgw` are installed

**Step 2: Run test to verify it fails**

Run: `bash scripts/release_install_test.sh`
Expected: FAIL because `install.sh` does not yet implement release installation.

### Task 2: Implement the repository-root installer

**Files:**
- Modify: `install.sh`

**Step 1: Write minimal implementation**

Implement:
- dependency checks for `curl`, `tar`, `mktemp`, and a SHA-256 tool
- OS/arch detection for `darwin/linux` and `amd64/arm64`
- environment-variable-controlled repo and API base URLs
- latest/tag release resolution via GitHub API
- asset download and checksum verification
- extraction and install to `~/.codex-gateway/bin`
- `cgw` symlink refresh
- binary verification via `codexgateway help`

**Step 2: Run test to verify it passes**

Run: `bash scripts/release_install_test.sh`
Expected: PASS

### Task 3: Add the release workflow

**Files:**
- Create: `.github/workflows/release.yml`

**Step 1: Add workflow**

Create a tag-triggered workflow that:
- builds the 4 target binaries
- archives them into tarballs with installer-compatible names
- generates `checksums.txt`
- publishes all assets to the GitHub Release for the tag

**Step 2: Verify workflow shape**

Run: `sed -n '1,240p' .github/workflows/release.yml`
Expected: includes the 4-target matrix and uploads both archives and `checksums.txt`.

### Task 4: Document release installation

**Files:**
- Modify: `README.md`

**Step 1: Update docs**

Document:
- local development build via `./scripts/build.sh`
- end-user remote install via `curl -fsSL .../install.sh | bash`
- optional `VERSION=vX.Y.Z` usage
- supported platforms and current Windows limitation

**Step 2: Verify docs references**

Run: `rg -n "build\\.sh|curl -fsSL|VERSION=|GitHub Releases|darwin|linux" README.md`
Expected: local and remote install paths are both documented clearly.
