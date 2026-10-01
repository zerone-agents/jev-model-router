# Issue #22：真实阿里云推理参数验收

2026-10-01 使用用户提供的阿里云 OpenAI 兼容端点，验证本地修复版 Router。未经修改的本地 agent-sdk 版本为 **3.5.1**。仅使用固定问候任务，输出限额 1024；没有包装或改写 SDK 的 fetch，也没有修改 SDK 源码。

## 结果

每个单元格均包含 JSON 与 SSE 两种调用；所有调用均取得非空正文、非空推理文本和正常结束状态。

| 上游模型 | 控制组合 | SDK 直连观测 | Router 显式选模 | Router auto |
| --- | --- | --- | --- | --- |
| qwen3.8-flash | thinking enabled | 通过 | 通过 | 通过 |
| qwen3.8-flash | effort high | 通过 | 通过 | 通过 |
| qwen3.8-flash | thinking enabled + effort high | 通过 | 通过 | 通过 |
| qwen3.8-max | thinking enabled | 通过 | 通过 | 通过 |
| qwen3.8-max | effort high | 通过 | 通过 | 通过 |
| qwen3.8-max | thinking enabled + effort high | 通过 | 通过 | 通过 |

表中为最终用于验收的 **36 个组合结果**。另有 **12 次 SDK 直接访问真实端点**的独立调用通过；观测工具修正期间的重跑不计入以上验收矩阵。[逐项脱敏证据](2026-10-01-reasoning.json)只保留模型、控制字段、状态及字符数，不含凭证、端点地址、消息或生成文本。

## 请求和响应核对

- 独立直连调用由 SDK 直接访问上游。字段观测另通过一个不改写请求体的本地反向代理完成；该基线路径不经过 Router/Bifrost，随后 Router 使用同一观测代理访问同一真实模型。
- SDK 3.5.1 实际发送 `max_tokens: 1024`，Router 按已有 #18 契约转换为 `max_completion_tokens: 1024`。没有提高、缩小或丢弃输出限额。
- `chat_template_kwargs.enable_thinking: true` 和 `reasoning_effort: "high"` 在直连及 Router 上游请求中一致，单独出现和同时出现均已核对。
- 上游 HTTP 状态为 200，原始 `finish_reason` 为 `stop`；SSE 检查到 `[DONE]`。SDK 收到 thinking/text 内容块，非流式终态为 `end_turn`，流式以 `done` 结束。
- 最终 Router 每个模型仅进行显式选模 6 次、auto 6 次上游调用，未发生生成重试或 fallback。
- 观测工具最初漏计了客户端关闭后的 SSE 收尾，并未解码直连压缩响应；工具修正后重新核对直连终态，Router 已保存的字段/终态证据通过离线比对。生产代码无需为这两个观测问题修改。

## 配置和范围

这两个模型在本次端点上验证的能力声明为：

```json
{
  "reasoning": [
    {"enable_thinking": true},
    {"reasoning_effort": "high"},
    {"enable_thinking": true, "reasoning_effort": "high"}
  ]
}
```

`auto` 测试同时配置了一个不支持显式推理控制的候选，并验证其被排除；实际生成走唯一合格候选路径，未调用真实 Jev。多候选决策输入/预算、其他 effort 值、thinking=false、非法组合零调用及取消行为由本地回归覆盖，本次没有扩展其真实端点兼容承诺。

这次证明了已测请求被接收、控制字段完整到达上游、响应内容与终态可用；不用于证明不同 effort 值的模型质量差异，也不代表已部署 Router 已更新。凭证临时文件在测试结束后清理，未写入仓库。
