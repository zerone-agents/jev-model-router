#!/usr/bin/env python3
"""Recompute the published snapshot from recorded grades and token usage, offline."""
import hashlib
import json
from decimal import Decimal
from pathlib import Path


def main():
    root = Path(__file__).resolve().parent
    raw = (root / 'snapshot.json').read_bytes()
    expected = (root / 'snapshot.sha256').read_text().split()[0]
    if hashlib.sha256(raw).hexdigest() != expected:
        raise ValueError('Snapshot integrity check failed')
    data = json.loads(raw)
    rows = data['answers']
    bank = {(r['task'], r['model']): r for r in rows}
    routes = data['routes']
    if len(routes) != 128 or len(rows) != 256 or len(bank) != 256:
        raise ValueError('Expected 128 tasks and exactly two answers per task')
    output = {}
    for policy in ('max', 'flash', 'auto', 'semantic'):
        passed, cost = 0, Decimal(0)
        for task in sorted(routes):
            model = policy if policy in ('max', 'flash') else routes[task][policy]
            r = bank[task, model]
            if not r['test_results'] or r['status'] != 200:
                raise ValueError('Missing test results or unsuccessful response')
            passed += all(v > 0 for v in r['test_results'])
            usage = r['usage']
            if usage.get('cache_creation_input_tokens', 0):
                raise ValueError('Unpriced cache creation usage')
            price = data['prices']['qwen3.8-' + model]
            cost += sum(Decimal(usage[key]) * Decimal(str(price[rate])) for key, rate in (
                ('input_tokens', 'input_per_million'),
                ('output_tokens', 'output_per_million'),
                ('cache_read_input_tokens', 'cache_read_per_million'),
            )) / Decimal(1000000)
        output[policy] = {'passed': passed, 'tasks': 128, 'generation_cny': str(cost)}
    print(json.dumps(output, indent=2))


if __name__ == '__main__':
    main()
