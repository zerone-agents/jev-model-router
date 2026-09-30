# CLI 安装

[English](cli-installation.md) | 简体中文

从 [GitHub Releases](https://github.com/zerone-agents/jev-model-router/releases) 下载匹配本机的压缩包。CLI 附件从 v0.1.1 开始提供，版本号与服务端一致。可执行文件同时包含远程管理命令和 `serve`，无需安装 Go 或 Node。

| 系统 | 架构 | 压缩包后缀 |
| --- | --- | --- |
| macOS Intel | amd64 | `darwin_amd64.tar.gz` |
| macOS Apple Silicon | arm64 | `darwin_arm64.tar.gz` |
| Linux x86-64 | amd64 | `linux_amd64.tar.gz` |
| Linux ARM64 | arm64 | `linux_arm64.tar.gz` |
| Windows x86-64 | amd64 | `windows_amd64.zip` |

同时下载该版本的 `SHA256SUMS`，解压前核对压缩包 SHA-256。macOS 使用 `shasum -a 256 <压缩包>`，Linux 使用 `sha256sum <压缩包>`，PowerShell 使用 `Get-FileHash <压缩包> -Algorithm SHA256`。校验和用于检查文件完整性，不是代码签名；二进制尚未签名或公证。

例如解压 `jev-router_0.1.1_darwin_arm64.tar.gz`，将其中的 `jev-router` 放入 PATH 目录。Windows 解压 ZIP 后可通过 PowerShell 运行 `jev-router.exe`，或将所在目录加入 PATH。

```sh
jev-router --version
jev-router --help
```

## 管理远程实例

使用服务器根地址，不带 `/v1`。通过 shell 或密钥管理器注入远端实例的 settings 凭证，不要放进命令参数：

```sh
export JEV_ROUTER_URL=https://router.example.com
# 安全设置 JEV_ROUTER_SETTINGS_TOKEN，值与远端 settings 凭证一致。
jev-router schema
jev-router call status.get --json - <<'JSON'
{}
JSON
jev-router dashboard
```

也可以保留云端 Compose 的 loopback 绑定，通过 SSH 隧道访问：

```sh
ssh -N -L 18080:127.0.0.1:8080 user@server
# 在另一个本地终端中：
export JEV_ROUTER_URL=http://127.0.0.1:18080
```

SQLite 和上游密钥留在服务器。本地 CLI 从本地读取 JSON 配置文件，通过管理 API 发送；供应商密钥引用则必须能在服务端解析。完整配置流程见[配套 SKILL](../skills/jev-router/SKILL.md)。

## 维护者

`CLI Release` 从标签对应代码打包五个平台，将压缩包和 `SHA256SUMS` 上传到对应 GitHub Release。Release 不存在时创建草稿，由维护者补充说明并决定公开时间。手动触发接受已有版本标签，可补发附件而无需移动标签。源码版本必须与标签一致。PR 仅构建验证、不上传。已有附件不会自动覆盖，重复上传会失败。

本地打包需要 Go 1.27.0 和 Python 3：

```sh
python3 scripts/package-cli.py --source /path/to/tag-checkout --version v0.1.1 --output /tmp/cli-assets
```

输出目录必须为空。CI 校验哈希并运行 Linux amd64 二进制；交叉编译通过不代表所有平台均已完成原生运行验证。

## 托管供应商密钥

此能力需要 v0.1.1 之后包含该功能的服务端/CLI，以及服务端主密钥；只安装 CLI 不会升级服务器。先检查 `jev-router schema providers.put` 是否提供 `api_key`。由密钥管理工具生成权限为 0600 的 JSON 文件，包含 `id`、`base_url` 和 `api_key`（与 `secret_ref` 必须二选一）。不要把密钥写进命令行参数或历史记录。读取 `status.get` 的当前版本后执行：

```sh
jev-router call providers.put --json /secure/provider.json \
  --expected-version VERSION --idempotency-key UNIQUE_OPERATION_KEY
```

使用 HTTPS，或经 SSH 隧道使用回环 HTTP。CLI 会拒绝通过公网 HTTP 提交内联密钥，也不会跟随重定向。读取 `providers.get` 核对脱敏状态，按 SKILL 测试禁用模型后再启用。更换上游 API Key 时提交相同供应商 ID、endpoint、新 `api_key`、当前版本和新的幂等键。结果未知时，在 24 小时内用完全相同的输入、版本和幂等键重试。仅改 endpoint 时保留响应中的托管 `secret_ref`，不要把脱敏显示值作为真实密钥。临时明文输入文件按你的密钥管理规范处理。

托管写入为原子操作，无需重启；在途请求保留旧凭证版本。主密钥与供应商 API Key 不同，必须独立备份，目前不支持原地轮换。详见[配置说明](configuration.md#managed-provider-credentials)。

CLI 继续使用 Settings Bearer 认证。浏览器将该凭证交换为 24 小时 HttpOnly 会话；远程访问需配置公开 HTTPS Origin。见[Dashboard 会话配置](configuration.md#dashboard-sessions)。
