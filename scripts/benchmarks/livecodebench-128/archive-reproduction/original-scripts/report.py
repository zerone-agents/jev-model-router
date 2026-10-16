import pathlib,json,statistics
D=pathlib.Path(__file__).parent;S=json.loads((D/'summary.json').read_text());T=json.loads((D/'per-task.json').read_text());labels={'max':'固定 Max','flash':'固定 Flash','auto':'Jev 回放','semantic':'SR 回放'}
lines=['# 冻结路由的三次生成共享答案库','', '使用原来的 128 道题及冻结的逐题模型选择，为每题 Max 和 Flash 各新增三次调用，建立 768 份新样本的共享答案库。原有样本没有混入本轮主分析。相同题目、相同模型、相同轮次的答案在两种策略间完全共享；同模型选择不再受到策略各自生成差异的干扰。','', '三轮标签事先指定，调用顺序交错随机化；并非先跑完一轮再看结果调整。temperature=0、thinking disabled、8192 Token 上限及并发 3 沿用原协议。独立指独立请求，不保证服务端随机性满足统计独立同分布。','', '## 三轮结果','', '本节费用仅为选定响应的生成费用；包含失败和补试的逐策略累计口径另列，未知用量不视为零。','', '| 轮次 | 固定 Max 通过 | 固定 Flash 通过 | Jev 通过 | SR 通过 | SR−Jev 题数 | Jev 生成 CNY | SR 生成 CNY | Jev−SR CNY |','|---|---:|---:|---:|---:|---:|---:|---:|---:|']
for k,r in S['replicates'].items():lines.append(f'| {k} | {r["max"]["passed"]}/128 | {r["flash"]["passed"]}/128 | {r["auto"]["passed"]}/128 | {r["semantic"]["passed"]}/128 | {r["sr_minus_jev_pass"]:+d} | {r["auto"]["generation_cny_known"]:.6f} | {r["semantic"]["generation_cny_known"]:.6f} | {r["jev_minus_sr_cny_known"]:+.6f} |')
lines+=['', '| 策略 | 三轮平均通过题数 | 每次生成平均通过率 | 平均每 128 题生成 CNY |','|---|---:|---:|---:|']
for a,label in labels.items():
 vals=[r[a] for r in S['replicates'].values()];p=statistics.mean(v['passed'] for v in vals);c=statistics.mean(v['generation_cny_known'] for v in vals);lines.append(f'| {label} | {p:.2f}/128 | {p/128:.2%} | {c:.6f} |')
lo,hi=S['task_stratified_bootstrap_95_pass_rate_difference'];diffs=[r['sr_minus_jev_pass'] for r in S['replicates'].values()];fees=[r['jev_minus_sr_cny_known'] for r in S['replicates'].values()]
lines+=['',f'SR 相对 Jev 的三轮题数差为 {diffs}，平均 {S["mean_sr_minus_jev_pass_count"]:+.2f} 题；生成费用节省分别为 '+', '.join(f'¥{v:+.6f}' for v in fees)+f'，平均 ¥{S["mean_jev_minus_sr_cny_known"]:+.6f}。','',f'基于每题三次平均差的分层成对 bootstrap 95% 区间：SR−Jev 通过率差 [{lo*100:+.2f}, {hi*100:+.2f}] 个百分点。该区间重采样任务，不是三个轮次的简单正态区间，也未充分估计未来服务端生成随机性。', '', ('三轮均观察到 SR 成绩更高。' if all(x>0 for x in diffs) else '三轮没有全部观察到 SR 成绩更高，应保留差异方向的波动。')+('三轮均观察到 SR 选定响应的生成费用更低。' if all(x>0 for x in fees) else '三轮没有全部观察到 SR 选定响应的生成费用更低。')+'这只是当前题集、配置和三次新采样的重复性证据，不能直接推出普遍质量非劣效或长期稳定优势。']
ci=S['task_stratified_bootstrap_95_mean_batch_saving']
if ci:lines+=['',f'每 128 题平均生成费用节省的分层 bootstrap 95% 区间：¥[{ci[0]:+.6f}, {ci[1]:+.6f}]。']
e=S['different_route_task_evidence'];lines+=['',f'54 道选择不同的题中，三次经验通过次数支持 SR {e["sr_higher_empirical_success"]} 题、支持 Jev {e["jev_higher_empirical_success"]} 题，{e["ties_among_different_routes"]} 题打平。这按经验成功次数比较，不是单题能力的显著性检验。']
lines+=['','## 模型自身波动','', '| 模型 | 0/3 通过题数 | 1/3 | 2/3 | 3/3 |','|---|---:|---:|---:|---:|']
for model,h in S['model_success_histograms'].items():lines.append('| '+model+' | '+' | '.join(str(h.get(str(i),0)) for i in range(4))+' |')
lines+=['', '1/3 和 2/3 是本次样本中结果发生变化的题；3/3 不证明通过概率为 100%，0/3 不证明绝对不会。主指标没有挑选三次中最好的答案，也没有把任一次通过记作每次生成通过。','', '## 费用和失败口径','', f'共享答案库实际发起 {S["actual_bank_attempts"]} 次请求，已知生成等价费用 CNY {S["actual_bank_generation_cny_known"]:.6f}，另有 {S["actual_bank_unknown_usage"]} 次用量未知。所有尝试 HTTP 状态：{S["actual_attempt_status"]}。接口失败最多补试一次，首次失败完整保留；从未根据正确性、输出截断或费用重试。','', '回放费用表示策略从同一答案库选出的响应费用，并不是另行支付的四组费用，不能相加为本轮账单。价格沿用 2026-10-13 冻结 Token 标价，不是 Coding Plan 实付。Jev 决策和 SR 本地路由均没有再次调用；此前的路由成本没有当作本轮新成本，也没有假定实际部署开销为零。本轮只能比较生成部分，不能给出完整端到端费用结论。缓存状态来自整个共享采样日程，回放没有模拟各策略独立部署时的缓存命中分布。']
lines+=['', '按策略归集的全部尝试费用（包含其选中模型的失败与补试；仍为共享库回放，并非额外调用）：','', '| 轮次 | 策略 | 全部尝试数 | 已知生成 CNY | 用量未知尝试数 |','|---|---|---:|---:|---:|']
for k,policies in S['replay_all_attempt_costs'].items():
 for a,v in policies.items():lines.append(f'| {k} | {labels[a]} | {v["attempts"]} | {v["generation_cny_known"]:.6f} | {v["unknown_usage_attempts"]} |')
lines+=['', '若任一策略存在未知失败用量，不能把选定成功响应的费用差说成已确认的完整重试后总费用优势。未知部分不是零，也没有根据成功补试的 Token 数反推失败消耗。']
for k,r in S['replicates'].items():
 for a,v in r.items():
  if isinstance(v,dict) and v.get('generation_usage_unknown'):lines.append(f'- 第 {k} 轮 {labels[a]}：{v["generation_usage_unknown"]} 次生成用量未知，费用仅为已知部分。')
lines+=['', '## 全部逐题回放','', '成功次数为该模型三次新生成通过数；同模型题两策略成绩必然相同。均值题差为 SR−Jev，费用差正值表示 SR 更省。','', '| 题目 | Jev/SR 选择 | Max 成功 | Flash 成功 | 平均题差 | 平均 Jev−SR CNY |','|---|---|---:|---:|---:|---:|']
for t in T:
 c=t['jev_minus_sr_mean_cny'];lines.append(f'| {t["id"]} | {t["routes"]["auto"]}/{t["routes"]["semantic"]} | {t["models"]["max"]["successes"]}/3 | {t["models"]["flash"]["successes"]}/3 | {t["sr_minus_jev_mean_pass"]:+.3f} | {f"{c:+.6f}" if c is not None else "未知"} |')
lines+=['', '## 验证与范围','', '- 使用独立目录，原配置与结果保持不变；原归档哈希核验记录见 source-integrity.json。','- 冻结文件、模型描述、提示词及配置版本核验见 validation.json。所有生成使用显式模型，无新增路由决策。','- 评分继承上一轮最终方案：固定镜像和官方提取器，单测试 6 秒，评分器基线加 2 GiB 虚拟分配预算，4 GiB 容器、单工作进程。全部代码在无网络、非 root、只读根文件系统中执行。','- 该题集已用于复盘，因此本轮是重复性诊断，不是新的未见测试集。没有根据这轮结果修改路由。','- 冻结的逐题选择只评估选择策略在不同生成样本上的表现，没有测量 Jev/SR 决策本身的随机性。','- 证据：attempts/ 全部尝试；attempt-selection.json 选定来源；bank.json 全部评分和用量；per-task.json 三次结果；summary.json 汇总及区间；scores/ 评分输出和容器状态。','']
(D/'report.zh-CN.md').write_text('\n'.join(lines));print('REPORT_WRITTEN')
