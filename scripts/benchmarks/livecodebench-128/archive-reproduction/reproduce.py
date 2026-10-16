#!/usr/bin/env python3
"""Recompute the best-observed report offline; never modify source archives."""
import argparse, hashlib, json, pathlib, shutil, subprocess, sys

def digest(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for b in iter(lambda: f.read(1024 * 1024), b''): h.update(b)
    return h.hexdigest()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data-root', type=pathlib.Path, default=pathlib.Path(__file__).resolve().parent.parent)
    parser.add_argument('--output', type=pathlib.Path, required=True, help='New directory; must not exist')
    a = parser.parse_args()
    root = a.data_root.resolve()
    shared = root / 'benchmark-shared-3x-2026-10-15'
    live = root / 'benchmark-expanded-128-2026-10-15'
    pins = json.loads((pathlib.Path(__file__).parent / 'source-pins.json').read_text())
    verified = {}
    for src in (shared, live):
        manifest = src / 'sha256.json'
        if digest(manifest) != pins[src.name]: raise RuntimeError('Source manifest changed: ' + str(src))
        entries = json.loads(manifest.read_text())
        for name, expected in entries.items():
            p = (src / name).resolve()
            if not p.is_relative_to(src.resolve()) or digest(p) != expected:
                raise RuntimeError('Source integrity failure: ' + str(p))
        verified[src.name] = len(entries)
    a.output.mkdir(parents=True, exist_ok=False)
    calc = a.output / 'recomputed'
    calc.mkdir()
    for name in ('manifest.json', 'routes.json', 'prices.json', 'attempt-selection.json', 'selection.json', 'summarize.py'):
        shutil.copy2(shared / name, calc / name)
    for name in ('scores', 'attempts'): shutil.copytree(shared / name, calc / name)
    with (a.output / 'analysis.log').open('w') as log:
        subprocess.run([sys.executable, str(calc / 'summarize.py')], check=True, stdout=log)
    s = json.loads((calc / 'summary.json').read_text())
    if s != json.loads((shared / 'summary.json').read_text()): raise RuntimeError('Recomputed summary differs')
    live_scores = json.loads((live / 'evaluation-recovered/summary.json').read_text())
    names = {'max': '固定 Max', 'flash': '固定 Flash', 'auto': 'Jev 冻结路由回放', 'semantic': 'SR 冻结路由回放'}
    best, means = {}, {}
    lines = ['# 128 题最佳观测成绩报告', '', '保存日期：2026-10-16。范围：同一批冻结的 128 道 LiveCodeBench 题及同一 Max / Flash 模型池。', '', '## 最佳完整轮次', '', '每个策略从三轮共享答案库回放中独立选择通过数最高的一轮；并列时选最早一轮。费用取该轮对应费用。没有逐题挑选最好答案，也没有把最低费用与最高成绩拼接。不同策略的最佳轮次不同，本表不是配对实验，也不代表期望成绩。', '', '| 策略 | 最佳通过数 | 通过率 | 轮次 | 所选响应生成费用（CNY） | 该策略该轮失败尝试用量未知数 |', '|---|---:|---:|---:|---:|---:|']
    for p, name in names.items():
        r = max(('1', '2', '3'), key=lambda k: s['replicates'][k][p]['passed'])
        v = s['replicates'][r][p]
        best[p] = dict(v, replicate=int(r), all_attempt_accounting=s['replay_all_attempt_costs'][r][p])
        means[p] = {key: sum(s['replicates'][k][p][key] for k in ('1', '2', '3')) / 3 for key in ('passed', 'generation_cny_known')}
        lines.append(f"| {name} | {v['passed']}/128 | {v['passed']/128:.2%} | {r} | {v['generation_cny_known']:.7f} | {best[p]['all_attempt_accounting']['unknown_usage_attempts']} |")
    lines += ['', '## 三轮完整结果与均值', '', '| 策略 | 第 1 轮 | 第 2 轮 | 第 3 轮 | 平均通过数 / 128 | 平均生成费用（CNY） |', '|---|---:|---:|---:|---:|---:|']
    for p, name in names.items():
        counts = ' | '.join(str(s['replicates'][k][p]['passed']) for k in ('1', '2', '3'))
        lines.append(f"| {name} | {counts} | {means[p]['passed']:.2f} | {means[p]['generation_cny_known']:.7f} |")
    lines += ['', 'Jev 与 SR 的平均通过数分别为 87.33 和 86.67。SR 减 Jev 通过率差的分层配对 bootstrap 95% 区间为 [-4.43, +3.39] 个百分点；没有稳定质量胜出的证据。SR 在三轮中的所选响应生成费用均更低，平均少约 16.01%，但未包含完整失败尝试费用和路由运行成本。', '', '## 原在线四组测试（独立展示）', '', '以下为此前同一批题的在线调用及网络故障恢复后的结果；Jev / SR 在该实验中实际执行路由。不能将上面的离线回放最高分宣称为在线实测成绩。', '', '| 策略 | 通过数 | 所选响应生成费用（CNY） |', '|---|---:|---:|']
    for p, name in names.items():
        v = live_scores[p]
        lines.append(f"| {name.replace(' 冻结路由回放', '')} | {v['official_extraction_pass']}/128 | {v['generation_cny']:.7f} |")
    lines += ['', '在线 Jev 另有已知决策费用 USD 0.010447668；SR 本机算力未定价。此前失败尝试存在未知用量，不能把表内费用视为完整总费用。', '', '## 协议与复现边界', '', '- 路由配置与每题选择冻结；Jev 和 SR 各选 Flash 43 次、Max 85 次，54 题的具体选择不同。本次回放没有新增路由判断，不测路由决策重复性。', '- 每题每模型三次新生成，共 768 个有效答案、770 次实际请求；两次 502 的用量未知。仅网络失败允许重试，不因错误答案或截断重试。', '- temperature=0，thinking disabled，max_tokens=8192，stream=false，生成并发 3；独立调用不等于供应商随机性满足 IID。', '- 使用冻结价格（2026-10-13）：Flash 输入/输出/缓存读取每百万 token 为 CNY 0.8/2.7/0.1；Max 为 CNY 12/36/1.5。来源和口径保存在原 prices.json。费用是 API 等价标价，不是 Coding Plan 实际账单。', '- 共享答案库实际已知生成费用 CNY 31.1051951，另有两次未知用量。各策略回放费用不相加；缓存命中来自交错生成日程，未模拟各策略单独运行时的缓存分布。', '- 题目为开发集校准后冻结的 64 道 Medium、64 道 Hard；只保证此前本项目未测，不保证训练数据未见。两套路由均使用校准配置，不代表产品默认设置。', '- 评分使用官方代码提取、6 秒测试超时、固定 evaluator commit 28fef95ea8c9f7a547c8329f2cd3d32b92c1fa24，以及固定镜像 sha256:085ee4d71dd0144099a7c077c867f7d260ae183b605f23c82287f880c8739c66。最终统一资源策略为 4 GiB 容器、评测进程基线以上额外 2 GiB 虚拟分配预算。', '- reproduce.py 校验两个原档案的哈希，再从原始响应与已保存的逐题评分结果重算费用、共享库、三轮比较和本报告；不调用模型、不执行候选代码、不重新评分。重新生成或评分请参见 README.md 和 original-scripts。', '- 最佳值是事后选择的观测上限，报告保留完整三轮与均值；不得据此宣称稳定超过固定 Max 或 SR。', '']
    (a.output / 'report.zh-CN.md').write_text('\n'.join(lines))
    result = {'best_observed': best, 'means': means, 'live_results': live_scores, 'verified_source_files': verified, 'source_manifest_sha256': pins}
    (a.output / 'results.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
    print(json.dumps({'best': {p: v['passed'] for p, v in best.items()}, 'verified': verified}))

if __name__ == '__main__': main()
