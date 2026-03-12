# GitHub Release Installer Design

## 背景

当前仓库已经将本地开发构建职责收敛到 `scripts/build.sh`。接下来需要补上面向终端用户的远程安装路径：用户不拉源码，只执行一条安装命令，即可从 GitHub Releases 下载对应平台的二进制并安装到本地。

## 目标

- 仓库根 `install.sh` 成为正式的远程安装入口
- 支持 4 个目标平台：
  - `darwin-amd64`
  - `darwin-arm64`
  - `linux-amd64`
  - `linux-arm64`
- 默认安装 latest release
- 支持通过环境变量指定版本，例如 `VERSION=v0.1.0`
- 校验下载文件的 checksum
- 安装到 `~/.codex-gateway/bin/codexgateway`
- 刷新 `~/.codex-gateway/bin/cgw` 软链

## 非目标

- Windows 安装
- shell rc 注入
- shell completion 自动安装
- Homebrew、winget 等包管理器接入
- 多二进制/插件式发布

## 方案对比

### 方案 1：GitHub Release + tar.gz + checksums + install.sh

每个平台构建一个 `tar.gz`，压缩包内只包含 `codexgateway` 主二进制，同时生成统一的 `checksums.txt`。安装脚本按平台选择正确的资产并校验哈希。

优点：

- 发布结构清晰
- 安装脚本实现简单
- 便于后续接 Homebrew
- `tar.gz` 能保留权限和扩展空间

缺点：

- 需要维护资产命名规范
- 需要一份校验文件

### 方案 2：GitHub Release + 裸二进制

不做打包，直接上传二进制。

优点：

- 最省事

缺点：

- 资产结构脆弱
- 后续加 README、LICENSE 或额外文件不方便
- 某些下载链路对权限保留不稳定

### 方案 3：先接包管理器

优点：

- 用户体验更标准

缺点：

- 发布复杂度明显升高
- 当前阶段收益不高

## 选型

采用方案 1。

## 发布物设计

Release 中上传以下资产：

- `codexgateway_darwin_amd64.tar.gz`
- `codexgateway_darwin_arm64.tar.gz`
- `codexgateway_linux_amd64.tar.gz`
- `codexgateway_linux_arm64.tar.gz`
- `checksums.txt`

`checksums.txt` 使用 `sha256sum` 兼容格式：

```text
<sha256>  codexgateway_darwin_amd64.tar.gz
<sha256>  codexgateway_darwin_arm64.tar.gz
...
```

## 安装脚本设计

仓库根 `install.sh` 负责：

1. 检测系统与架构
2. 将平台映射到发布资产名
3. 解析目标版本
   - 默认 `latest`
   - 若设置 `VERSION`，则使用指定 tag
4. 下载压缩包与 `checksums.txt`
5. 校验对应资产的 SHA256
6. 解压二进制
7. 安装到 `~/.codex-gateway/bin/codexgateway`
8. 更新 `cgw` 软链
9. 运行 `codexgateway help` 做基础验证

依赖要求：

- `curl`
- `tar`
- `uname`
- `mktemp`
- `shasum -a 256` 或 `sha256sum`

为便于测试，脚本支持通过环境变量覆写：

- `REPO_OWNER`
- `REPO_NAME`
- `GITHUB_API_BASE`
- `GITHUB_DOWNLOAD_BASE`

这样可以在本地测试中指向伪造的 release 响应和静态文件目录，而不依赖真实 GitHub。

## GitHub Actions 设计

新增 tag 触发的 workflow：

- 触发条件：`v*` tag push
- 构建矩阵：
  - `darwin/amd64`
  - `darwin/arm64`
  - `linux/amd64`
  - `linux/arm64`
- 构建输出：
  - `dist/<asset-name>/codexgateway`
  - `dist/<asset-name>.tar.gz`
- 汇总校验：
  - 生成 `dist/checksums.txt`
- 发版：
  - 使用 GitHub Actions 官方 release/upload 方案将资产上传到对应 tag release

## 错误处理

安装脚本在以下情况直接失败并给出明确提示：

- 当前平台不受支持
- 缺少必要命令
- 指定版本不存在
- 下载失败
- checksum 缺失或不匹配
- 压缩包内缺少 `codexgateway`

## 测试策略

### 脚本测试

新增 shell 测试脚本，覆盖：

- 平台映射正确
- `VERSION` 指定行为
- `latest` 路径解析
- checksum 校验通过
- 安装后二进制与软链存在

测试通过本地临时 HTTP 服务返回伪造 release JSON 和静态资产。

### 工作流静态检查

本地不执行完整 GitHub Release，但至少验证：

- workflow YAML 存在
- 资产命名与安装脚本一致
- README 的安装文档与脚本参数一致

## 用户入口

本地开发：

```bash
./scripts/build.sh
```

远程安装：

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/install.sh | bash
```
