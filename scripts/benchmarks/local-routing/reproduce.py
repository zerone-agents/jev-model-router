"""Print the report tables offline using only the Python standard library."""
import hashlib
import json
import math
import runpy
import statistics
from decimal import Decimal
from pathlib import Path

HERE = Path(__file__).resolve().parent


def verified(path, checksum):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != checksum.read_text().split()[0]:
        raise ValueError(f"Checksum mismatch: {path.name}")
    return json.loads(raw)


def stats(values):
    return f"{statistics.median(values):,.1f} / {sorted(values)[math.ceil(.95 * len(values)) - 1]:,.1f}"


def main():
    evidence = verified(HERE / 'evidence.json', HERE / 'evidence.sha256')
    policies = {'auto': 'Jev Model Router (cloud Jev)', 'semantic': 'Semantic Router',
                'laya': 'Jev Model Router (Laya)', 'decider': 'Jev Model Router (Decider 2B)',
                'd3': 'Jev Model Router (D3 nano)', 'max': 'Fixed Max', 'flash': 'Fixed Flash'}
    cells = {p: [] for p in policies}
    all_ids = set()
    for suite, count in [('livecodebench-128', 128), ('noncode-96', 96)]:
        folder = HERE.parent / suite
        s = verified(folder / 'snapshot.json', folder / 'snapshot.sha256')
        code = count == 128
        if code:
            routes = s['routes']
        else:
            tasks = {t['id']: t for t in s['tasks']}
            grade = runpy.run_path(str(folder / 'reproduce.py'))['grade']
            routes = {t: {} for t in tasks}
        prices = s['prices'] if code else s['prices']['generation']
        bank = {}
        for row in s['answers']:
            t, m = row['task'], row['model'].split('-')[-1]
            if not code and row['arm'] in ('auto', 'semantic'):
                routes[t][row['arm']] = m
                continue
            assert row['status'] == 200 and (t, m) not in bank
            if code:
                assert row['test_results']
                passed = all(v > 0 for v in row['test_results'])
            else:
                passed = grade(row['final_line'], tasks[t]['kind'], tasks[t]['gold'])[0]
            usage, rates = row['usage'], prices['qwen3.8-' + m]
            assert not usage.get('cache_creation_input_tokens', 0)
            cost = sum(usage[k + '_tokens'] * Decimal(str(rates[r + '_per_million']))
                       for k, r in [('input','input'), ('output','output'), ('cache_read_input','cache_read')])
            bank[t, m] = passed, cost / 1000000
        assert len(routes) == count and len(bank) == count * 2
        all_ids.update(routes)
        for p in policies:
            passed = nmax = 0
            cost = Decimal(0)
            for t in routes:
                m = p if p in ('max','flash') else (routes[t][p] if p in ('auto','semantic') else evidence['routes'][t][p])
                ok, c = bank[t, m]
                passed += ok
                cost += c
                nmax += m == 'max'
            cells[p].extend([str(passed), f'{nmax} / {count-nmax}', f'{cost:.4f}'])
    assert all_ids == set(evidence['routes'])
    print('| Strategy | Code /128 | Max / Flash | CNY | Non-code /96 | Max / Flash | CNY |')
    print('|---|---:|---:|---:|---:|---:|---:|')
    for p, name in policies.items():
        print('| ' + ' | '.join([name] + cells[p]) + ' |')
    print('\n| Decision backend | RTX 4090 | Four CPU cores | Online API (historical) |')
    print('|---|---:|---:|---:|')
    sequence = None
    for p in ('laya','decider','d3'):
        times = []
        for device in ('gpu','cpu'):
            rows = evidence['latency'][device + '-' + p]
            assert [r[0] for r in rows] == list(range(25)) and all(r[2] == 200 for r in rows)
            ids = [r[1] for r in rows[1:]]
            assert ids == ids[:8]*3 and len(set(ids)) == 8 and set(ids) <= all_ids
            assert sequence is None or sequence == ids
            sequence = ids
            times.append(stats([r[3] for r in rows[1:]]))
        print('| ' + ' | '.join([policies[p], *times, '—']) + ' |')
    history = dict(evidence['cloud_ms'])
    assert len(history) == len(evidence['cloud_ms']) == 96
    print('| Cloud Jev | — | — | ' + stats(list(history.values())) + ' |')


if __name__ == '__main__':
    main()
