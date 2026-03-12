# Banner Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Codex Gateway 的 CLI Banner 替换为由 `cli-ascii-logo` 预生成的 Logo，并根据环境在纯文本与 ANSI 成品之间切换。

**Architecture:** 在 `internal/ui` 内固化两份 Banner 资产，并将环境检测封装为可测试的 helper。CLI 各命令改为把实际输出 writer 传入 UI 层，以便按目标终端能力选择 Banner。

**Tech Stack:** Go, `golang.org/x/term`, existing CLI tests

---

### Task 1: 为 Banner 选择逻辑写失败测试

**Files:**
- Create: `internal/ui/ui_test.go`
- Modify: `internal/ui/ui.go`

**Step 1: Write the failing test**

新增测试覆盖：
- `NO_COLOR` 时返回 plain banner
- 非 TTY writer 时返回 plain banner
- TTY writer 且允许颜色时返回 ANSI banner

**Step 2: Run test to verify it fails**

Run: `go test ./internal/ui`

Expected: FAIL，因为当前 `ui.Banner()` 不接受 writer，也没有环境分支。

**Step 3: Write minimal implementation**

为 UI 包增加：
- `Banner(w io.Writer) string`
- `bannerForWriter(...)`
- `shouldUseANSIBanner(...)`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/ui`

Expected: PASS

### Task 2: 固化预生成 plain/ANSI Banner 并替换调用点

**Files:**
- Modify: `internal/ui/ui.go`
- Modify: `internal/cli/init.go`
- Modify: `internal/cli/auth.go`
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/process.go`

**Step 1: Write the failing test**

先跑现有 CLI 测试，确认受影响调用点在签名变化后会失败。

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli ./cmd/server`

Expected: FAIL，因 `ui.Banner()` 签名变化导致编译失败或测试失败。

**Step 3: Write minimal implementation**

把所有调用点更新为向 `ui.Banner(...)` 传入实际 writer。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli ./cmd/server`

Expected: PASS

### Task 3: 做针对性回归验证

**Files:**
- No code changes required unless verification fails

**Step 1: Run focused verification**

Run: `go test ./internal/ui ./internal/cli ./cmd/server`

Expected: PASS

**Step 2: Run full project verification**

Run: `go test ./...`

Expected: PASS
