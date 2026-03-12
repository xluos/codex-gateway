# Codex Gateway Shell Completion Design

## 背景

`codexgateway` 当前基于 Cobra 构建命令树，但没有暴露 shell completion 能力，也没有在安装流程中接入补全脚本。因此用户在 `zsh`、`bash`、`fish` 中按 `Tab` 时，拿不到子命令和 flag 补全。

## 目标

- 提供标准 `completion` 子命令，支持 `zsh`、`bash`、`fish`
- 在安装脚本中自动为当前 shell 安装补全
- 重复运行 `scripts/install.sh` 保持幂等，不重复追加 shell 配置
- 保留手动安装路径，用户可直接运行 `codexgateway completion <shell>`

## 非目标

- 不支持 PowerShell 或 Elvish
- 不在本次需求中做动态参数值补全
- 不引入额外的包管理器集成逻辑

## 方案

### CLI 层

在根命令下增加 `completion` 子命令，使用 Cobra 原生 completion 生成功能输出对应 shell 的脚本。命令形态为：

- `codexgateway completion bash`
- `codexgateway completion zsh`
- `codexgateway completion fish`

根命令现有子命令和 flag 会自动进入补全集，不需要手写静态脚本。

### 安装层

安装脚本在构建二进制后，生成补全脚本到 `~/.codex-gateway/completions/`：

- `codexgateway.bash`
- `_codexgateway`
- `codexgateway.fish`

然后按当前 shell 将一段带 marker 的 `source` 片段追加到对应 rc 文件中。若 marker 已存在，则不重复追加。脚本文件每次安装都允许覆盖更新，这是期望的幂等行为。

### `cgw` 兼容

`cgw` 是指向 `codexgateway` 的软链。completion 安装先保证 `codexgateway` 可补全。本次不单独生成 `cgw` 专属 completion 脚本，避免增加别名级兼容复杂度。若后续需要，再评估 shell alias/function 方案。

## 错误处理

- 若 shell 类型不在支持列表内，安装脚本跳过 completion 接入并打印提示
- 若 rc 文件不存在，则创建
- 若生成 completion 脚本失败，安装流程直接失败，避免给出半配置状态

## 测试策略

- Go 测试验证 `completion` 子命令存在，并能为 `zsh` 输出非空脚本
- Shell 级测试验证 `scripts/install.sh` 重复执行不会重复写入 completion marker
- 跑全量 `go test ./...` 确认 CLI 行为无回归
