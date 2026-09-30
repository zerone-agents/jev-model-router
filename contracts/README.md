# 管理能力契约

机器契约在 `management.json` 和 `schemas/` 中，服务端校验与实例能力发现使用同一来源。`GET /admin/v1/schema`、`GET /admin/v1/schema/{capability}` 和 `POST /admin/v1/call/{capability}` 均要求 settings 凭证。UI 复用这些管理能力，提供状态、模型描述、提示词和路由记录的查看，以及描述/提示词编辑。

## 最小能力集合

| 业务目标 | 结果与副作用 |
| --- | --- |
| 发现能力 | 返回版本、描述、schema、认证、风险及可用状态 |
| 读取状态 | 返回实例与依赖状态，不泄露凭证 |
| 列举/读取模型与供应商 | 分页、按权限过滤，返回模型描述和能力 |
| 更新模型/供应商配置 | 校验并原子提交新配置版本 |
| 读取/更新路由提示词 | 读取当前版本，写入用户修改；默认仅一份均衡模板 |
| 检查路由选择 | 返回选择结果，不调用生成模型、不改变会话；可能调用决策 API |
| 测试模型连接 | 对指定模型发送固定最小生成请求，可测试禁用模型；不接受任意对话，不自动启用，可能产生费用与外发 |
| 查询路由记录 | 返回有界、分页的结构化记录，不包含原始对话或决策自由文本理由 |

首版管理能力使用可信管理员 `settings` 凭证；`inference` 仅提供模型列表与生成，能使用实例内全部启用模型。两者是操作分工，不是互不信任租户之间的隔离。

配置按资源修改，保存前校验完整配置，成功即发布全局新版本，不自动发起网络检查。写入携带 `expected_version` 与幂等键，成功修改、版本递增和幂等结果在同一事务提交；成功结果保留 24 小时并可跨重启重放。命中成功记录先于版本检查；校验失败不占用幂等键，修正重试仍检查版本。版本冲突与幂等冲突分别使用 `config_conflict`、`idempotency_conflict`。

CLI 的 `--help` 离线可用，`schema` 发现实例实际能力；复杂输入支持 JSON 文件或 stdin，结果默认 JSON、日志走 stderr，不弹交互确认，不隐藏重试。这些行为已由 CLI/HTTP 及端到端测试覆盖。

## 模型 ID 约束

`auto` 是自动路由入口的保留对外 ID，生成模型无论是否启用均不得使用。模型创建及任何涉及对外 ID 的配置变更，必须由机器 schema 声明并由服务端校验拒绝，失败不改变配置或版本。此限制不针对上游模型名称。机器 schema、服务端事务校验及运行测试覆盖保留名；首版 ID 稳定，put 按 ID 创建或完整替换，不提供 rename。

## 每项契约必须描述

- 稳定 capability ID、description、输入输出 JSON Schema；参数约束、必填项和示例。
- 所需认证与 scopes，副作用及费用/外传可能，L0–L3 风险与审批责任方。
- 同步/异步结果语义、`operation_id`、稳定错误 code、`retryable` 与修复建议。
- 列表分页与有界输出，写入幂等键、预期配置版本及冲突规则。

能力风险按实际影响分类，不能将所有“更新配置”机械地标成低风险。新增外传目的地、放宽敏感保护或改变权限，需要比修改普通模型描述更严格的执行边界。支持何种审批机制在实现该能力前确定并声明，不伪造审批句柄。

只读 schema/status 不调用付费模型；选模检查不是无副作用的纯本地预览。任何异步能力只有在状态查询和终态都实现后才声明可用。

## 响应约定

管理结果使用 `ok`、`data` 或 `error`、`warnings`、`meta`，其中 meta 包含 operation ID、timestamp 和 schema version；首版为同步管理能力，未暴露异步事件协议。CLI 将结构化错误映射到稳定非零退出码，并在契约中公布映射。

这些约定只用于管理能力。`/v1/chat/completions` 保持其 JSON/SSE 格式，`/v1/models` 保持模型列表格式。公共模型列表不携带供应商凭证与管理元数据。

参考：[AgentUse 0.2.0 协议页面](https://www.zerone.run/zh/protocol)。本目录不表示已获得协议认证。

## 请求与输出 schema

call body 为 `{"input":{...},"expected_version":1,"idempotency_key":"operation-key"}`；读操作只需 input。能力的 input_schema 验证 input，output_schema 描述成功响应的 data。写入返回 version/resource；models.test 的 data.ok 表示探测结果，顶层 ok 表示管理操作是否完成。错误和退出码在 schemas/results.json 中。

风险为声明给调用方的元数据，不是内置审批引擎。settings 是可信管理员，客户端按用户授权决定是否执行。schema/status 不产生上游调用，route.inspect/models.test 的外发与费用在能力描述中列出。

每项运行时能力包含 `call.method`、`call.path`、完整 `call.schema` 和 `call.protocol`。调用信封由 schemas/call.json 定义并用于写入边界校验；能力 input_schema 嵌入 call.schema.input。call.protocol 公布全局版本冲突恢复、成功幂等重放顺序/身份范围/24小时期限、失败不占键及通用 HTTP/CLI 错误映射。幂等期限与服务端过期计算来自同一 results.json 定义。仅查询单项 schema 也能获取完整调用与重试规则。

提示词长度统一按 JSON Schema maxLength 的 Unicode 字符数计算（上限16384），保存整个配置时复用 prompt.put 校验。Jev 的序列化字节预算独立计算：可以保存的提示词仍可能使多候选决策超限，此时明确返回 budget_exceeded。

供应商详情 `providers.get` 返回只读 endpoint 和 `api_key_masked`：密钥超过 8 个字符时仅显示前 4 个字符及 `***`，短密钥全部打码，无法解析时返回 null。完整密钥不会进入管理响应，脱敏字段不参与配置写入。
