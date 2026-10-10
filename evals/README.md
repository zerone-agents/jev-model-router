# 显式评测

普通测试和 CI 不发起真实评测；运行器单元测试只使用本地模拟服务。先按实例模型卡校准 cases.jsonl 的 `fast`/`deep` 可接受集合及人工质量判据，固定评测期间的配置；样例标签不是客观最优模型结论。

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


## 仅选模检查

`--mode inspect`只调用`route.inspect`，需要settings凭证，不需要inference凭证，不生成答案；仍需`--allow-paid`。`--repeat N`仅用于inspect模式。输出保留轮次、类别、配置版本、选择路径与候选ID；`quality`仍为unknown。调用失败或返回候选外ID写入error行，运行完成不代表验收通过，必须检查全部结果及预先固定的门槛。

公开的[选模回归集](routing/README.zh-CN.md)提供运行方法和历史门槛；[Agent合成任务](agent/README.zh-CN.md)提供多轮复现协议；[评测说明](../docs/evaluation.md)定义证据与评分边界。所有真实结果写入已忽略的`docs/validation/`，不要提交原始日志。

根目录的旧`cases.jsonl`是带小输出上限的接口样例，不是本次完整答案质量基线。质量评测应记录并区分截断，不能用这些样例的预算判断模型能力。`auto`/`fixed`模式只生成非流式单次响应且不保存答案，不能替代多轮Agent或完整流式验收。
