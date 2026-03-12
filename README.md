# OpenAI Local Gateway

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

1. 准备配置文件

```bash
cd /Users/bytedance/Documents/AIWorkspace/openai-local-gateway
cp config.example.yaml config.yaml
```

2. 修改 `config.yaml`

- `auth.api_keys`: 你本地客户端要使用的 key
- `upstream.mode=api_key` 时，填写 `upstream.api_key`
- `upstream.mode=oauth` 时，先执行登录命令生成 `credentials/openai-oauth.json`

3. 启动服务

```bash
cd /Users/bytedance/Documents/AIWorkspace/openai-local-gateway
go run ./cmd/server -config ./config.yaml
```

## OAuth 登录

如果你不想在 `config.yaml` 里放上游 API Key，可以切到：

```yaml
upstream:
  mode: oauth
```

然后执行：

```bash
cd /Users/bytedance/Documents/AIWorkspace/openai-local-gateway
go run ./cmd/server auth login -config ./config.yaml
```

成功后，凭证会写到：

```text
./credentials/openai-oauth.json
```

查看当前凭证状态：

```bash
go run ./cmd/server auth status -config ./config.yaml
```

手动刷新凭证：

```bash
go run ./cmd/server auth refresh -config ./config.yaml
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
cd /Users/bytedance/Documents/AIWorkspace/openai-local-gateway
go test ./...
```

如果你已经完成 OAuth 登录，可以直接跑 smoke：

```bash
cd /Users/bytedance/Documents/AIWorkspace/openai-local-gateway
./scripts/smoke_oauth.sh ./config.yaml your-local-api-key
```

脚本会自动启动一个临时服务实例，验证：

- `/v1/models`
- `/v1/responses`
- `/v1/chat/completions`

如果目标端口已经有旧进程，脚本会直接报错退出，避免你把请求打到旧服务上。
