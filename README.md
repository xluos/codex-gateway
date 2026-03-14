# Codex Gateway

Codex Gateway 是一个面向本地单用户场景的 OpenAI 兼容网关，用于在本机暴露统一的 OpenAI 风格接口，并将请求转发到上游 OpenAI 服务。

它保留了自用场景最需要的能力：

- 本地静态 API Key 鉴权
- 上游 `api_key` / `oauth` 两种认证模式
- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- 可选别名路由：`/models`、`/chat/completions`、`/responses`
- OAuth 模式下将 `chat/completions` 转换为 Codex Responses 上游请求
- 本地运行态管理、状态诊断与日志排查

这个项目刻意保持简单，不试图覆盖多租户网关、计费系统或生产级调度能力。

## 项目定位

Codex Gateway 适合以下场景：

- 你希望在本地统一接入 OpenAI 风格 API，而不在每个客户端里分别处理认证逻辑
- 你需要在 `api_key` 与 `oauth` 两种上游认证方式之间切换
- 你希望用一个轻量网关兼容依赖 `/v1/chat/completions` 或 `/v1/responses` 的本地工具

如果你的目标是多用户接入、配额控制、账号池、故障切换、审计和后台管理，这个项目不是合适的基础设施。

## 特性

- 轻量部署：单二进制运行，无数据库、无 Redis、无额外服务依赖
- OpenAI 兼容接口：暴露常见模型查询与生成接口，便于对接现有客户端
- 双认证模式：支持上游 API Key，也支持 OAuth 登录与刷新
- OpenAI 多账号池：支持按模型选账号，并在额度不足或限流时自动切换
- 本地运行控制：提供 `start`、`stop`、`restart`、`status`、`logs`、`doctor` 等命令
- 可调试日志：支持请求摘要日志，必要时可打开 HTTP 调试转储

## 非目标

以下能力当前不在项目范围内：

- 多用户与多租户
- 订阅、配额、计费
- 多平台账号调度与复杂故障切换
- Web 管理后台
- 完整的 OAuth 管理平台能力

## 快速开始

### 1. 本地构建

在项目根目录执行：

```bash
./scripts/build.sh
```

本地构建脚本会：

- 编译二进制到 `~/.codex-gateway/bin/codexgateway`
- 创建缩写命令 `~/.codex-gateway/bin/cgw`
- 检查二进制是否可正常执行

如果 `~/.codex-gateway/bin` 已经在你的 `PATH` 里，构建完成后即可直接初始化：

```bash
codexgateway init
```

仓库根目录的 `./install.sh` 用于 GitHub Releases 远程安装，不用于本地开发构建。

### 2. 远程安装

终端用户可以直接执行：

```bash
curl -fsSL https://raw.githubusercontent.com/bytedance/codex-gateway/main/install.sh | bash
```

如需安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/bytedance/codex-gateway/main/install.sh | VERSION=v0.1.0 bash
```

当前远程安装支持：

- macOS amd64
- macOS arm64
- Linux amd64
- Linux arm64

暂不支持 Windows。

### 3. 初始化配置

首次使用建议执行：

```bash
codexgateway init
```

如需指定配置文件路径：

```bash
codexgateway init -config /path/to/config.yaml
```

初始化向导会交互式询问：

- 本地网关 API Key
- 上游认证模式：`oauth` 或 `api_key`
- `api_key` 模式下的上游 OpenAI API Key

在不确定本地环境是否完整时，可以先运行：

```bash
codexgateway doctor
```

### 4. 启动服务

前台运行：

```bash
codexgateway serve
```

后台运行：

```bash
codexgateway start
```

项目也保留了 Go 开发模式入口：

```bash
go run ./cmd/server -config ~/.codex-gateway/config.yaml
```

默认配置下，服务监听在 `http://127.0.0.1:9867`。

### 5. 调用示例

查询模型：

```bash
curl http://127.0.0.1:9867/v1/models \
  -H 'Authorization: Bearer local-dev-key'
```

调用 `chat/completions`：

```bash
curl http://127.0.0.1:9867/v1/chat/completions \
  -H 'Authorization: Bearer local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}'
```

## 配置说明

仓库提供了示例配置文件 `config.example.yaml`。

核心配置项包括：

- `server`
  - 本地监听地址、端口以及读写超时
- `auth.api_keys`
  - 允许访问本地网关的 API Key 列表
- `upstream`
  - 上游模式、基础地址、API Key 与请求超时
- `upstreams`
  - OpenAI 多账号池配置；非空时优先于旧 `upstream`
  - 每个账号可配置 `priority`、`default_model`、`model_mapping`、`cooldown_seconds`
- `oauth`
  - 本地 OAuth 回调地址、凭证文件位置与浏览器自动打开选项
- `runtime`
  - PID、日志与状态文件路径
- `logging`
  - 日志级别与 HTTP 调试转储开关
- `compat`
  - 是否启用兼容别名路由

默认运行态文件位于：

- `~/.codex-gateway/codex-gateway.pid`
- `~/.codex-gateway/codex-gateway.json`
- `~/.codex-gateway/codex-gateway.log`

多账号示例：

```yaml
upstreams:
  - name: primary
    mode: api_key
    base_url: https://api.openai.com
    api_key: sk-primary
    priority: 10
    cooldown_seconds: 300
    default_model: gpt-4.1
    model_mapping:
      gpt-4.1: gpt-4.1

  - name: backup
    mode: api_key
    base_url: https://api.openai.com
    api_key: sk-backup
    priority: 20
    cooldown_seconds: 300
    default_model: gpt-4.1-mini
```

行为说明：

- `upstreams` 非空时，网关会从账号池中选 OpenAI 账号转发请求
- 账号可通过 `model_mapping` 显式声明支持模型
- 没有 `model_mapping` 的账号会被视为通用账号
- 请求未携带 `model` 时，网关会使用所选账号的 `default_model`
- 当上游返回 `insufficient_quota`、`rate_limit`、`billing_hard_limit` 等额度相关错误时，网关会将当前账号短暂冷却并切换到下一个可用账号

## CLI 命令

常用命令如下：

```bash
codexgateway help
codexgateway init
codexgateway serve
codexgateway start
codexgateway stop
codexgateway restart
codexgateway doctor
codexgateway status
codexgateway accounts status
codexgateway logs -n 100
```

认证相关命令：

```bash
codexgateway auth login
codexgateway auth status
codexgateway auth refresh
```

本地构建后同时提供缩写命令 `cgw`，例如：

```bash
cgw start
cgw doctor
cgw status
```

## OAuth 使用说明

如果你不希望在配置文件中保存上游 API Key，可以将配置切换为：

```yaml
upstream:
  mode: oauth
```

然后执行：

```bash
codexgateway auth login
```

登录成功后，凭证默认写入：

```text
~/.codex-gateway/openai-oauth.json
```

查看当前凭证状态：

```bash
codexgateway auth status
```

查看多账号池状态：

```bash
codexgateway accounts status
```

该命令返回每个账号的可用性与额度摘要，例如当前是否处于 cooldown，以及最近一次记录到的 5 小时 / 7 天 usage snapshot。

手动刷新凭证：

```bash
codexgateway auth refresh
```

## 日志与排障

服务默认记录每个请求的摘要信息，包括：

- 本地路径
- 上游路径
- 状态码
- 请求耗时
- 上游 `x-request-id`

如需更详细的排查信息，可在配置中打开：

```yaml
logging:
  debug_dump_http: true
```

开启后会额外记录归一化后的请求体，以及上游错误响应体片段。这个选项只建议在本地临时排障时使用，因为日志中可能包含敏感请求内容。

无论使用前台 `serve` 还是后台 `start`，日志都会写入 `runtime.log_file`。前台模式会同时输出到终端，后台模式只写入日志文件。

## 开发与验证

运行测试：

```bash
go test ./...
```

如果已经完成 OAuth 登录，可以执行 smoke 测试：

```bash
./scripts/smoke_oauth.sh ~/.codex-gateway/config.yaml your-local-api-key
```

该脚本会临时启动网关实例，并验证：

- `/v1/models`
- `/v1/responses`
- `/v1/chat/completions`

如果目标端口上已经有旧实例在运行，脚本会直接退出，避免误把请求发送到错误进程。

## 卸载

如需移除本地命令入口：

```bash
./scripts/uninstall.sh
```

卸载脚本会移除：

- `~/.codex-gateway/bin/codexgateway`
- `~/.codex-gateway/bin/cgw`
- shell rc 文件中的历史 PATH 配置块（如果之前旧版安装脚本写入过）

它不会删除你的配置文件、日志文件或 OAuth 凭证。若需要彻底清理，可手动删除 `~/.codex-gateway`。

## Roadmap

当前项目以本地单用户可用性为主。后续如果继续演进，更值得优先考虑的是：

- 更完整的配置校验与错误提示
- 更清晰的协议兼容性说明
- 更稳定的本地运维体验与诊断输出

相比之下，过早引入多租户、计费或复杂调度，会明显增加项目复杂度，并破坏当前仓库最有价值的轻量属性。

## Contributing

欢迎提交 issue 和 PR。提交前建议先阅读 [CONTRIBUTING.md](./CONTRIBUTING.md)，其中说明了项目的变更边界、最小验证要求，以及提交时应避免带入的本地敏感文件。

## Security

如果发现安全问题，请不要公开提交漏洞细节。处理方式见 [SECURITY.md](./SECURITY.md)。

## License

本项目采用 [GNU GPL v3.0](./LICENSE) 许可协议发布。

这意味着：

- 你可以使用、修改和分发本项目
- 分发修改版本时，必须继续以 GPL-3.0 方式开放源码
- 本项目不提供任何明示或暗示担保

如果你的目标是将其合并到闭源产品中，GPL-3.0 通常并不适合该场景，请在使用前自行评估合规影响

## English Summary

Codex Gateway is a lightweight local OpenAI-compatible gateway designed for single-user usage. It exposes `/v1/models`, `/v1/chat/completions`, and `/v1/responses`, supports both upstream `api_key` and `oauth` authentication modes, and provides local process management commands such as `start`, `stop`, `status`, `logs`, and `doctor`.

This project is intentionally scoped for personal or local tooling scenarios. It does not aim to provide multi-tenant routing, billing, quota management, account pools, or an admin console. For local development, run `./scripts/build.sh`. For end-user installation, run `curl -fsSL https://raw.githubusercontent.com/bytedance/codex-gateway/main/install.sh | bash` or pin a tag with `VERSION=vX.Y.Z`. Then initialize the config with `codexgateway init` and start the gateway with `codexgateway serve` or `codexgateway start`.
