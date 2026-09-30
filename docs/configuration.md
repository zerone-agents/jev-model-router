# 启动配置

运行时业务配置由管理 API 保存到 SQLite，环境变量和 JSON conf 只负责启动配置。优先级为 `JEV_ROUTER_<字段大写>` 环境变量 > JSON conf > 默认值。

| JSON 字段 | 默认值 |
| --- | --- |
| listen | 127.0.0.1:8080 |
| database | .data/router.sqlite |
| settings_token_ref | env:JEV_ROUTER_SETTINGS_TOKEN |
| inference_token_ref | env:JEV_ROUTER_INFERENCE_TOKEN |
| encryption_key_ref | 空（仅使用外部凭证时无需设置） |
| decision_timeout | 10s |
| first_event_timeout | 60s |
| idle_timeout | 60s |
| record_timeout | 100ms |
| max_body_bytes | 16777216 |
| stream_buffer | 8 |
| retention_days | 7 |

以上是通过本地边界测试的首版默认值，不代表真实模型延迟承诺。时长接受 Go duration 字符串；数值限额必须为正。

两种 Token 必须存在且不同。凭证引用支持 `env:NAME` 或 `file:/absolute/path`；文件末尾换行会去除。settings 是可信管理员，能改变生成请求目的地；不应交给不可信用户。这两种认证 Token 不存 SQL。供应商密钥可以使用外部引用，或按下方托管模式加密保存到 SQL；均不以明文回显于状态或错误。

普通禁用只影响新请求，紧急停止需要停止实例或撤销上游密钥。默认仅监听本机，暴露到网络时由部署方提供适当的 TLS 边界。


CLI `--url` 优先于 `JEV_ROUTER_URL`，未设置时使用启动配置的 listen 地址。配置文件只供启动和 CLI 连接凭证引用；providers/models/decision/prompt 通过管理 API 保存。Jev base_url 为根地址（例如 `https://api.typesafe.ai`），生成 provider base_url 为兼容前缀（例如 `https://api.openai.com/v1`）。

官方 Jev 配置使用 `https://api.typesafe.ai` 根地址和固定模型版本 `jev-1.13.0`。适配器追加 `/v1/systemone`，因此不要在决策 base_url 中再次填写 `/v1`。将以下 JSON 作为 `decision.put` 的输入，并由运行环境注入密钥：

```json
{"base_url":"https://api.typesafe.ai","model":"jev-1.13.0","secret_ref":"env:TYPESAFE_API_KEY"}
```

参见 [TypeSafe 官方 API 文档](https://docs.typesafe.ai/api)与[模型版本](https://docs.typesafe.ai/models)。生成供应商继续使用各自的 OpenAI 兼容前缀，与决策后端配置独立。

JSON 示例：

```json
{"listen":"127.0.0.1:8080","database":".data/router.sqlite","decision_timeout":"10s","first_event_timeout":"60s","idle_timeout":"60s"}
```

first_event_timeout 覆盖流建立到首个有效 SSE 事件，非流式及固定连接测试则覆盖完整响应；之后以 idle_timeout 约束事件等待及下游写入，不设健康流总时长上限。生成客户端最多保留 16 组供应商地址/密钥快照，满载时明确拒绝额外目标，不偷换在途配置。

记录每次尝试最多等待 record_timeout，保留清理在启动及每小时执行；失败令 status.get 的 records_degraded 置 true 并给出 warning，状态持续到重启。记录不是事务审计，不承诺无损。数据库用于单实例进程，请勿多进程共享同一文件。


## 管理页面

`serve` 在同源 `/dashboard/` 提供管理 UI，无需 Node 运行时。`dashboard [--url URL] [--config file] [--no-open]` 使用 CLI 相同的地址优先级；从 listen 构造地址时将通配监听映射为本机 loopback。URL 必须是 HTTP(S) 根地址，不能含凭证、查询或片段。

普通模式先执行最多 5 秒、无凭证且不跟随重定向的 HTML 可达检查，再通过系统浏览器打开；失败以 JSON 返回错误及有效地址供手动打开。`--no-open` 仅返回 JSON 地址，不执行探测，不启动服务。该命令不需要 settings 凭证。

浏览器单独输入 settings 凭证，仅在页面内存保存。刷新或断开连接后需重新输入；不写入 URL 或浏览器存储。UI 只访问同源 API，远程使用必须部署 HTTPS，本机 loopback HTTP 例外。UI 不管理账号、密钥或启动参数。

## Docker Compose

推荐部署入口见 [Quickstart](../quickstart/README.zh-CN.md)。镜像内监听 `0.0.0.0:8080`，数据库为 `/data/router.sqlite`，Compose 默认仅发布宿主机 `127.0.0.1:8080`，并用命名卷保存数据。镜像以 UID/GID 10001 运行；使用自定义 bind mount 时需确保该用户可写入数据库目录。

通过 `docker compose exec -T router jev-router …` 使用容器内 CLI，无需在宿主机安装 Go。JSON 文件通过 `--json -` 和 stdin 传入，宿主机路径不会自动出现在容器中。Compose 的 `.env` 仅为插值提供值；新增密钥引用对应的环境变量须显式注入服务。文件型引用需额外挂载文件并使用容器内绝对路径。

## Managed provider credentials

`encryption_key_ref` / `JEV_ROUTER_ENCRYPTION_KEY_REF` is an optional startup `env:` or absolute `file:` reference to a hex-encoded 32-byte AES-256-GCM master key. Configure it on the server only. Missing/empty environment material keeps legacy external-reference deployments working but rejects managed writes. Nonempty malformed keys, unreadable key files, unsupported references, or existing managed records without a matching key fail startup. All retained revisions are verified at startup, including records from deleted providers.

Quickstart uses `env:JEV_ROUTER_ENCRYPTION_KEY`; generate the value once with `openssl rand -hex 32`. Back up it separately from SQLite and restore the same key with the database. Master-key rotation/re-encryption is not implemented; replacing this value makes existing records unreadable. Upstream-key replacement through `providers.put` is supported without restart.

Provider writes accept exactly one of `api_key` and `secret_ref`. Inline keys are nonempty UTF-8, at most 16384 bytes, with no CR/LF/NUL. SQLite stores versioned authenticated ciphertext bound to the provider and immutable revision. Reads return references and masked status only; no reveal endpoint. `managed:` references belong to one provider and cannot be used for decision settings. Retaining such a reference preserves the key on metadata updates.

Key revisions are retained so captured request snapshots and successful idempotent receipts remain valid. Deleting a provider does not erase historical ciphertext or revoke upstream keys; revoke at the supplier for emergency response. Credential history garbage collection is deferred. Encryption protects a database-only leak, not a compromised running server with access to the master key. Keep TLS termination and proxy request-body logging configured accordingly. Neither requests nor keys belong in logs.
