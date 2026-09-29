# 显式评测

普通测试和 CI 不执行此工具。先按实例模型卡校准 cases.jsonl 的 `fast`/`deep` 可接受集合及人工质量判据，固定评测期间的配置；样例标签不是客观最优模型结论。

```sh
go run ./evals --cases evals/cases.jsonl --mode auto --output /tmp/auto-metrics.jsonl --allow-paid
go run ./evals --cases evals/cases.jsonl --mode fixed --model fast --output /tmp/fixed-metrics.jsonl --allow-paid
```

从 `JEV_ROUTER_URL`、`JEV_ROUTER_SETTINGS_TOKEN`、`JEV_ROUTER_INFERENCE_TOKEN` 读取目标与凭证。`--allow-paid` 是操作者对潜在费用的显式授权；Agent 仍需先取得用户的费用授权。

auto 模式先调用 route.inspect 获得一次 Jev 决策，再以该外部 ID 显式生成，以分开测量决策与生成 usage/耗时，避免重复付费选模。这测量规划与选中模型，完整 auto HTTP 路径由端到端测试覆盖。配置变化可能影响生成，评测期间保持配置稳定。

可选 `--prices prices.json`，价格按每百万 token，同一货币：

```json
{"decision":{"Input":0.1,"Output":0.1},"models":{"fast":{"Input":0.1,"Output":0.2}}}
```

结果仅写案例 ID、模型 ID、可接受集合命中、耗时、usage、费用及状态。缺失 usage/价格为 null/unknown；质量固定 unknown，另按 quality_criteria 人工评审，不能把集合命中或 Jev confidence 当成答案质量。工具不保存生成内容或原始请求，不重试，不覆盖既有结果文件。
