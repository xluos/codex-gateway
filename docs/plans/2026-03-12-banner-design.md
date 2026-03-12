# Codex Gateway Banner Design

## 背景

`codex-gateway` 当前在多个 CLI 命令中复用同一个 `ui.Banner()`，包括 `help`、`doctor`、`auth status/login` 和交互式初始化流程。现有 Banner 是一段简单的 ASCII 文本，由 `lipgloss` 统一着色。

本次需求不是在运行时调用 `cli-ascii-logo` skill，而是先生成一次 Logo，再把生成结果固化进 Go 代码中。同时保留两份静态资产：

- 无色 ASCII 文本
- 预生成 ANSI 成品

代码需要根据环境决定输出哪一份。

## 目标

- 用 `cli-ascii-logo` 生成的新 Logo 替换当前 Banner 视觉
- 不引入新的运行时依赖或外部命令调用
- `NO_COLOR` 和非 TTY 环境默认回落到无色文本
- 支持在交互式终端中优先显示预生成 ANSI Banner

## 非目标

- 不新增运行时参数，如 `--no-color`
- 不在当前需求中重构所有 CLI 输出样式
- 不把 Banner 迁移为动态生成逻辑

## 方案

### 静态资产

在 `internal/ui/ui.go` 中内嵌两份预生成结果：

- `bannerPlain`: 纯文本 box drawing + ASCII 文本
- `bannerANSI`: 预生成 ANSI TrueColor 渐变成品

两者都由 `cli-ascii-logo` 预先生成，运行时只做选择，不做重新渲染。

### 运行时选择逻辑

新增内部判断逻辑，根据以下规则选择 Banner：

1. 若设置了 `NO_COLOR`，使用 `bannerPlain`
2. 若输出 writer 不是终端，使用 `bannerPlain`
3. 其他情况优先使用 `bannerANSI`

为了让逻辑可测，不把环境读取和 TTY 检测硬编码在 `Banner()` 内部，而是拆成可注入的辅助函数。

### 调用点调整

将所有 `ui.Banner()` 调用改为 `ui.Banner(out)` 或 `ui.Banner(w)`，让 UI 层基于实际输出目标决定样式，而不是默认基于进程 stdout 推测。

## 测试策略

- 新增 `internal/ui/ui_test.go`
- 覆盖三类行为：
  - `NO_COLOR` 存在时返回无色文本
  - 非 TTY writer 时返回无色文本
  - TTY 且未禁色时返回 ANSI Banner

同时跑受影响的 CLI 测试，确认输出仍包含原有关键文案，不因 Banner 变化导致行为回归。

## 风险

- 预生成 ANSI 常量较长，可读性会下降
- 部分终端对 TrueColor 支持不完整，但已有非 TTY/`NO_COLOR` 回退
- 若后续继续改文案，需要重新生成 plain/ANSI 两份资产，不能只改其中一份
