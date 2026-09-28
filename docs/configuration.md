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

以上数值是首版实现初值，交付前通过本地超时和背压测试验证，不代表真实模型延迟承诺。时长接受 Go duration 字符串；数值限额必须为正。

两种 Token 必须存在且不同。凭证引用支持 `env:NAME` 或 `file:/absolute/path`；文件末尾换行会去除。settings 是可信管理员，能改变生成请求目的地；不应交给不可信用户。密钥不存 SQL，不回显于状态或错误。

普通禁用只影响新请求，紧急停止需要停止实例或撤销上游密钥。默认仅监听本机，暴露到网络时由部署方提供适当的 TLS 边界。
