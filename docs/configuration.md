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
| decision_max_bytes | 32000 |
| first_event_timeout | 60s |
| idle_timeout | 60s |
| record_timeout | 100ms |
| max_body_bytes | 16777216 |
| stream_buffer | 8 |
| retention_days | 7 |
| record_max_count | 100000 |

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

记录每次尝试最多等待 record_timeout。`retention_days`（1–3650，默认 7）限制保留天数；`record_max_count`（1–10000000，默认 100000）限制记录总数，对应环境变量 `JEV_ROUTER_RETENTION_DAYS` 和 `JEV_ROUTER_RECORD_MAX_COUNT`，重启生效。数量上限在每次写入事务内执行，超出后按写入顺序丢弃最旧记录；调低上限时启动阶段每批最多删除 1000 条。过期清理在启动及每小时分批执行，因此到期记录可能延迟至下一次清理移除。清理释放的 SQLite 页可供后续写入复用，不自动缩小数据库文件，也不承诺磁盘字节上限。后台清理或记录写入失败令 status.get 的 records_degraded 置 true 并给出 warning，状态持续到重启。记录不是事务审计，不承诺无损。数据库用于单实例进程，请勿多进程共享同一文件。


## 管理页面

`serve` 在同源 `/dashboard/` 提供管理 UI，无需 Node 运行时。`dashboard [--url URL] [--config file] [--no-open]` 使用 CLI 相同的地址优先级；从 listen 构造地址时将通配监听映射为本机 loopback。URL 必须是 HTTP(S) 根地址，不能含凭证、查询或片段。

普通模式先执行最多 5 秒、无凭证且不跟随重定向的 HTML 可达检查，再通过系统浏览器打开；失败以 JSON 返回错误及有效地址供手动打开。`--no-open` 仅返回 JSON 地址，不执行探测，不启动服务。该命令不需要 settings 凭证。

浏览器单独输入 Settings 凭证并交换为固定 7 天的 HttpOnly 会话，刷新或同凭证重启后可恢复；显式注销在服务端撤销当前请求会话。认证材料不写入 URL 或 Web Storage。UI 只访问同源 API，远程使用需部署 HTTPS 并配置公开 Origin，本机回环 HTTP 例外。UI 不管理账号、密钥或启动参数；完整生命周期与恢复规则见下方 Dashboard sessions。

## Docker Compose

推荐部署入口见 [Quickstart](../quickstart/README.zh-CN.md)。镜像内监听 `0.0.0.0:8080`，数据库为 `/data/router.sqlite`，Compose 默认仅发布宿主机 `127.0.0.1:8080`，并用命名卷保存数据。镜像以 UID/GID 10001 运行；使用自定义 bind mount 时需确保该用户可写入数据库目录。

通过 `docker compose exec -T router jev-router …` 使用容器内 CLI，无需在宿主机安装 Go。JSON 文件通过 `--json -` 和 stdin 传入，宿主机路径不会自动出现在容器中。Compose 的 `.env` 仅为插值提供值；新增密钥引用对应的环境变量须显式注入服务。文件型引用需额外挂载文件并使用容器内绝对路径。

## Managed provider credentials

`encryption_key_ref` / `JEV_ROUTER_ENCRYPTION_KEY_REF` is an optional startup `env:` or absolute `file:` reference to a hex-encoded 32-byte AES-256-GCM master key. Configure it on the server only. Missing/empty environment material keeps legacy external-reference deployments working but rejects managed writes. Nonempty malformed keys, unreadable key files, unsupported references, or existing managed records without a matching key fail startup. All retained revisions are verified at startup, including records from deleted providers.

Quickstart uses `env:JEV_ROUTER_ENCRYPTION_KEY`; generate the value once with `openssl rand -hex 32`. Back up it separately from SQLite and restore the same key with the database. Master-key rotation/re-encryption is not implemented; replacing this value makes existing records unreadable. Upstream-key replacement through `providers.put` is supported without restart.

Provider writes accept exactly one of `api_key` and `secret_ref`. Inline keys are nonempty UTF-8, at most 16384 bytes, with no CR/LF/NUL. SQLite stores versioned authenticated ciphertext bound to the provider and immutable revision. Reads return references and masked status only; no reveal endpoint. `managed:` references belong to one provider and cannot be used for decision settings. Retaining such a reference preserves the key on metadata updates.

Key revisions are retained so captured request snapshots and successful idempotent receipts remain valid. Deleting a provider does not erase historical ciphertext or revoke upstream keys; revoke at the supplier for emergency response. Credential history garbage collection is deferred. Encryption protects a database-only leak, not a compromised running server with access to the master key. Keep TLS termination and proxy request-body logging configured accordingly. Neither requests nor keys belong in logs.

## Dashboard sessions

The dashboard exchanges the Settings credential for a host-only HttpOnly, SameSite=Strict cookie scoped to `/admin/`. Sessions expire exactly 7 days after login, without renewal. Refresh/reopen and server restarts with the same Settings credential and origin preserve valid sessions. CLI continues to use Bearer authentication. No browser authentication material is written to Web Storage.

Set `JEV_ROUTER_DASHBOARD_ORIGIN=https://router.example.com` (or JSON `dashboard_origin`) for remote access. Use an origin only, with optional port. The environment overrides the configuration file. Without it only loopback HTTP browser sessions are allowed; remote CLI remains available. Remote origins require HTTPS; explicit HTTP is allowed only for localhost/literal loopback IPs.

A TLS reverse proxy must preserve the original Host. Keep the HTTP container backend private. Secure cookies follow the configured public HTTPS origin even over private HTTP; X-Forwarded-* does not grant trust. Browser requests require exact same-origin checks, with a session-bound CSRF header for every POST. No credentialed CORS is provided.

Logout revokes only its request session. Its now-useless cookie may remain until expiry or the next login: logout and error responses never write/clear cookies, so a delayed response cannot erase another tab's newer login. A stale tab's CSRF cannot revoke a newer session; reconnect explicitly without automatic write/logout retry. Network or storage failure does not confirm revocation; retain the session and retry logout. If login has an unknown outcome, check session status before another authentication action.

At most 128 active sessions are allowed. Expiry/logout frees capacity; replacement login can replace its own session at capacity. `session_limit` requires an existing logout or expiry, not automatic retries. Changing Settings or the origin policy on restart invalidates all sessions, including when an old Settings value is later restored. Inference-only rotation leaves sessions intact. When restoring a database backup, use a new Settings credential to invalidate backed-up sessions. Provider encryption keys are unrelated to session authentication.

## Jev 决策输入预算

`decision_max_bytes` 控制序列化后的 Jev 请求字节预算，环境变量为 `JEV_ROUTER_DECISION_MAX_BYTES`。例如 JSON 配置 `{"decision_max_bytes":64000}` 或环境变量 `JEV_ROUTER_DECISION_MAX_BYTES=64000`；环境变量优先，需重启。取值为 1 至 268435456 的整数；增大本地预算不改变上游限制。

2026-10-03 核对 [Jev 官方模型文档](https://docs.typesafe.ai/models)：请求总上下文 64k tokens，state 加最长问题 32k tokens。本项目只发一个选模问题，所以参照 32k；官方未给出字节预算推荐值。默认 32,000 **字节**是本地按每 token 一字节的保守启发式，不是官方 token 限制的精确换算，也不保证上游一定接受。需要更高利用率时，可按实际语言、模型和上游验证结果配置更大字节预算。

预算包含候选、偏好、系统指令、最新 query、问题与序列化开销。Jev 不接收工具定义；工具历史仅保留调用 ID、工具名称及结果关联记录，不包含参数、输出或执行消息的附带文本。较旧历史按完整组省略；system/developer 与最新 query 的各文本段独立保留首尾、从中间截断，插入 [...truncated...] 并标记 content_truncated。按实际序列化字节数调整共同文本上限，小段保持完整，中文与 emoji 不拆分。生成请求不裁剪；候选、要求及最小首尾结构仍超限时返回 budget_exceeded。auto 多候选和 route.inspect 使用相同预算，显式模型与单候选不调用 Jev。

Generation providers accept `protocol: "openai" | "anthropic"`. Omitted protocol defaults to `openai`, including replacement writes; preserve it when editing an Anthropic provider. Reads return the effective protocol. Existing stored rows need no migration or version increment. `base_url` is the API prefix (for example `https://api.anthropic.com/v1`).

### Native Anthropic generation upstream

Use a complete `providers.put` input such as:

```json
{"id":"claude","protocol":"anthropic","base_url":"https://api.anthropic.com/v1","secret_ref":"env:ANTHROPIC_API_KEY"}
```

The adapter appends `/messages`; OpenAI providers append `/chat/completions`. No hostname or version-path guessing occurs. `providers.get` returns the effective protocol and derived `endpoint`. Omission of protocol on replacement resets it to openai. Managed `api_key` writes support the same protocol field.

Native requests without an output limit send **65536** tokens and reserve the same amount in automatic routing. Explicit limits are preserved, never clamped. Connectivity probes still use 16. Use `reasoning_effort:"none"` or `chat_template_kwargs:{"enable_thinking":false}` for tool requests, including continuation. Single-turn thinking can use true (adaptive), but signed thinking history and thinking+tools are unsupported. Test model support explicitly; a local conversion check does not guarantee the upstream model accepts its parameters.
