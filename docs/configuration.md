# 启动配置

运行时业务配置由管理 API 保存到 SQLite，环境变量和 JSON conf 只负责启动配置。优先级为 `JEV_ROUTER_<字段大写>` 环境变量 > JSON conf > 默认值。

| JSON 字段 | 默认值 |
| --- | --- |
| listen | 127.0.0.1:8080 |
| database | .data/router.sqlite |
| settings_token_ref | env:JEV_ROUTER_SETTINGS_TOKEN |
| inference_token_ref | env:JEV_ROUTER_INFERENCE_TOKEN |
| decision_timeout | 10s |
| first_event_timeout | 60s |
| idle_timeout | 60s |
| record_timeout | 100ms |
| max_body_bytes | 16777216 |
| stream_buffer | 8 |
| retention_days | 7 |

以上是通过本地边界测试的首版默认值，不代表真实模型延迟承诺。时长接受 Go duration 字符串；数值限额必须为正。

两种 Token 必须存在且不同。凭证引用支持 `env:NAME` 或 `file:/absolute/path`；文件末尾换行会去除。settings 是可信管理员，能改变生成请求目的地；不应交给不可信用户。密钥不存 SQL，不回显于状态或错误。

普通禁用只影响新请求，紧急停止需要停止实例或撤销上游密钥。默认仅监听本机，暴露到网络时由部署方提供适当的 TLS 边界。


CLI `--url` 优先于 `JEV_ROUTER_URL`，未设置时使用启动配置的 listen 地址。配置文件只供启动和 CLI 连接凭证引用；providers/models/decision/prompt 通过管理 API 保存。Jev base_url 为根地址（例如 `https://api.typesafe.ai`），生成 provider base_url 为兼容前缀（例如 `https://api.openai.com/v1`）。

JSON 示例：

```json
{"listen":"127.0.0.1:8080","database":".data/router.sqlite","decision_timeout":"10s","first_event_timeout":"60s","idle_timeout":"60s"}
```

first_event_timeout 覆盖流建立到首个有效 SSE 事件，非流式及固定连接测试则覆盖完整响应；之后以 idle_timeout 约束事件等待及下游写入，不设健康流总时长上限。生成客户端最多保留 16 组供应商地址/密钥快照，满载时明确拒绝额外目标，不偷换在途配置。

记录每次尝试最多等待 record_timeout，保留清理在启动及每小时执行；失败令 status.get 的 records_degraded 置 true 并给出 warning，状态持续到重启。记录不是事务审计，不承诺无损。数据库用于单实例进程，请勿多进程共享同一文件。
