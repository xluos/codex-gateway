# Codex Gateway

从 `sub2api` 的 OpenAI 网关思路里提炼出的本地最小版，只保留单用户自用场景需要的能力。

## 支持范围

- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- 可选别名路由：`/models`、`/chat/completions`、`/responses`
- 本地静态 API Key 鉴权
- 上游 OpenAI `api_key` / `oauth` 两种认证模式
- `auth login` / `auth status` / `auth refresh`
- OAuth 模式下将 `chat/completions` 转换到 Codex Responses 上游
- 请求摘要日志；打开 `logging.debug_dump_http=true` 后会额外打印归一化请求体和上游错误体

不包含：

- 数据库 / Redis
- 管理后台
- 多用户、订阅、配额、计费
- 多账号调度与故障切换
- OAuth 管理链路

## 使用方式

## 安装

本地编译并安装二进制：

```bash
cd /Users/bytedance/Documents/AIWorkspace/codex-gateway
./scripts/install.sh
```

安装脚本会做这些事：

- 编译二进制到 `~/.codex-gateway/bin/codexgateway`
- 创建缩写命令 `~/.codex-gateway/bin/cgw`
- 如果 `~/.codex-gateway/config.yaml` 不存在，就自动用示例配置创建
- 把 `~/.codex-gateway/bin` 写进你的 shell rc 文件，默认是 `~/.zshrc`

安装完成后执行：

```bash
source ~/.zshrc
codexgateway help
cgw help
```

如果你以后想移除命令入口，可以执行：

```bash
cd /Users/bytedance/Documents/AIWorkspace/codex-gateway
./scripts/uninstall.sh
```

注意：

- 安装脚本只能修改 shell 配置文件，不能直接改掉“当前已经打开的 shell 进程”的环境变量，所以安装后需要 `source` 一次。
- 安装脚本不会删除你的 `~/.codex-gateway/config.yaml`、日志和 OAuth 凭证，卸载时只会移除二进制和 PATH 配置块。

1. 准备配置文件

```bash
mkdir -p ~/.codex-gateway
cp /Users/bytedance/Documents/AIWorkspace/codex-gateway/config.example.yaml ~/.codex-gateway/config.yaml
```

2. 修改 `~/.codex-gateway/config.yaml`

- `auth.api_keys`: 你本地客户端要使用的 key
- `upstream.mode=api_key` 时，填写 `upstream.api_key`
- `upstream.mode=oauth` 时，先执行登录命令生成 `credentials/openai-oauth.json`

3. 启动服务

```bash
codexgateway serve
```

也保留了兼容旧用法：

```bash
go run ./cmd/server -config /path/to/config.yaml
```

## CLI 命令

查看帮助：

```bash
codexgateway help
```

后台启动：

```bash
codexgateway start
```

查看状态：

```bash
codexgateway status
```

查看最近日志：

```bash
codexgateway logs -n 100
```

停止服务：

```bash
codexgateway stop
```

重启服务：

```bash
codexgateway restart
```

运行态文件默认会写到：

- `~/.codex-gateway/codex-gateway.pid`
- `~/.codex-gateway/codex-gateway.json`
- `~/.codex-gateway/codex-gateway.log`

## OAuth 登录

如果你不想在 `config.yaml` 里放上游 API Key，可以切到：

```yaml
upstream:
  mode: oauth
```

然后执行：

```bash
codexgateway auth login
```

成功后，凭证会写到：

```text
~/.codex-gateway/openai-oauth.json
```

查看当前凭证状态：

```bash
codexgateway auth status
```

手动刷新凭证：

```bash
codexgateway auth refresh
```

## 日志排查

服务会默认输出每个请求的摘要日志，包括：

- 本地路径
- 上游路径
- 状态码
- 请求耗时
- 上游 `x-request-id`

如果你要看更细的排查信息，可以打开：

```yaml
logging:
  debug_dump_http: true
```

开启后会额外记录归一化后的请求体，以及上游错误响应体片段。这个开关只建议本地自用时临时打开，因为它可能把你的 prompt 和请求参数打进日志。

无论前台 `serve` 还是后台 `start`，日志都会写到 `runtime.log_file`。默认是 `~/.codex-gateway/codex-gateway.log`。前台模式会同时输出到终端，后台模式只落盘。

4. 调用示例

```bash
curl http://127.0.0.1:8081/v1/models \
  -H 'Authorization: Bearer local-dev-key'
```

```bash
curl http://127.0.0.1:8081/v1/chat/completions \
  -H 'Authorization: Bearer local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}'
```

## 开发验证

```bash
cd /Users/bytedance/Documents/AIWorkspace/codex-gateway
go test ./...
```

如果你已经完成 OAuth 登录，可以直接跑 smoke：

```bash
cd /Users/bytedance/Documents/AIWorkspace/codex-gateway
./scripts/smoke_oauth.sh ~/.codex-gateway/config.yaml your-local-api-key
```

脚本会自动启动一个临时服务实例，验证：

- `/v1/models`
- `/v1/responses`
- `/v1/chat/completions`

如果目标端口已经有旧进程，脚本会直接报错退出，避免你把请求打到旧服务上。
