# 选模偏好回归

[English](README.md)

这里保留2026年9月选模实验的公开合成题。`development.jsonl`有8题，`regression.jsonl`有12题，后者在原实验中未参与调参。**公开后它就是回归集，不能继续当作独立验收集。** 调整提示词后应另建事前标注的新任务。

标签表示配置偏好：常规任务选`qwen3.8-flash`，复杂推理和正确性分析选`qwen3.8-max`；不代表另一模型做不出题目。`model-descriptions.json`采用Flash便宜、快，Max更强、更贵、可能更慢的验收假设，不是经济实测结论。

在独立实例中配置上述已启用模型ID和描述、相同能力（context_limit 16000，tools/structured_output为true，images为false）、未改动的[均衡模板](../../templates/balanced.md)，以及Jev `jev-1.13.0`。配置前发现当前管理schema。供应商密钥经服务端secret_ref加载，评测工具只需实例settings凭证；它不配置实例，也不调用生成端点。

```sh
# 实例另行启动并配置；安全注入其 settings token。
export JEV_ROUTER_URL=http://127.0.0.1:8080
mkdir -p docs/validation
# 可能产生 Jev 费用，不需要 inference 凭证。
go run ./evals --mode inspect --cases evals/routing/development.jsonl --output docs/validation/routing-development.jsonl --allow-paid
go run ./evals --mode inspect --cases evals/routing/regression.jsonl --repeat 2 --output docs/validation/routing-regression.jsonl --allow-paid
```

每轮使用新输出路径，不自动重试或覆盖。`repeat`区分重复轮次，按文件顺序运行（历史实验第二轮使用逆序）。进程成功退出只表示写出了指标，不等于验收通过。检查每题每轮都有成功记录、`path=jev_choice`、两名候选、相同`config_version`，且没有错误行。选中ID不在候选内会记为错误。运行器不能发现这些快照之外的配置变化，评测期间保持实例不变。

原定门槛：至少22/24次命中，每类至少10/12，至少10/12题重复选择一致，无调用或非法选择错误；开发集单独计分。每轮调用前固定并记录题集/模型描述/模板摘要、代码版本、服务端配置与门槛。运行器只保存指标，不写凭证、提示词或模型答案；原始运行文件不公开。

这里仅衡量选模偏好。生成质量使用独立Agent任务，实际经济收益另设专题。历史结论见[验收摘要](../../docs/acceptance/2026-09-29.zh-CN.md)。
