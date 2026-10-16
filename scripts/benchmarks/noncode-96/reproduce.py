#!/usr/bin/env python3
"""Regrade recorded final lines and recompute successful-response token costs offline."""
import argparse
from collections import Counter
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import re

ARMS = ('max', 'flash', 'auto', 'semantic')
MODELS = {'qwen3.8-max', 'qwen3.8-flash'}
KINDS = ('math', 'knowledge', 'reading')


def grade(line, kind, gold):
    patterns = {'math': r'FINAL: ([+-]?(?:\d+(?:\.\d*)?|\.\d+))',
                'knowledge': r'FINAL: ([A-J])', 'reading': r'FINAL: (true|false)'}
    match = re.fullmatch(patterns[kind], line)
    if not match:
        return False, False
    answer = match[1]
    return (Decimal(answer) == Decimal(gold) if kind == 'math' else answer == gold), True


def integer(value):
    if type(value) is not int or value < 0:
        raise ValueError('Token counts must be nonnegative integers')
    return value


def calculate(data):
    if data.get('schema_version') != 1:
        raise ValueError('Unsupported snapshot version')
    tasks = {t['id']: t for t in data['tasks']}
    if len(tasks) != 96 or len(data['tasks']) != 96 or Counter(t['kind'] for t in tasks.values()) != dict.fromkeys(KINDS, 32):
        raise ValueError('Expected 96 unique tasks, 32 per kind')
    expected = {(t, arm) for t in tasks for arm in ARMS}
    actual = [(r['task'], r['arm']) for r in data['answers']]
    if len(actual) != 384 or set(actual) != expected:
        raise ValueError('Expected exactly one response per task and arm')
    output = {}
    for arm in ARMS:
        passed, invalid, limited = 0, 0, 0
        cost, decision = Decimal(0), Decimal(0)
        by_kind = dict.fromkeys(KINDS, 0)
        selected = Counter()
        for row in data['answers']:
            if row['arm'] != arm:
                continue
            model = row['model']
            if row['status'] != 200 or model not in MODELS:
                raise ValueError('Unexpected response status or model')
            if arm in ('max', 'flash') and model != 'qwen3.8-' + arm:
                raise ValueError('Fixed-model arm changed model')
            task = tasks[row['task']]
            correct, valid = grade(row['final_line'], task['kind'], task['gold'])
            passed += correct
            by_kind[task['kind']] += correct
            invalid += not valid
            limited += row['stop'] == 'max_tokens'
            selected[model] += 1
            usage = row['usage']
            if integer(usage.get('cache_creation_input_tokens', 0)):
                raise ValueError('Unpriced cache creation usage')
            price = data['prices']['generation'][model]
            for key, rate in [('input_tokens', 'input_per_million'), ('output_tokens', 'output_per_million'),
                              ('cache_read_input_tokens', 'cache_read_per_million')]:
                cost += Decimal(integer(usage[key])) * Decimal(str(price[rate])) / Decimal(1000000)
            du = row['decision_usage']
            if du.get('complete') is not True or du.get('invocations') != (1 if arm == 'auto' else 0):
                raise ValueError('Unexpected or incomplete decision usage')
            for key, rate in [('input_tokens', 'input_per_million'), ('output_tokens', 'output_per_million')]:
                decision += Decimal(integer(du[key])) * Decimal(str(data['prices']['decision'][rate])) / Decimal(1000000)
        output[arm] = {'passed': passed, 'n': 96, 'by_kind': by_kind, 'models': dict(sorted(selected.items())),
                       'generation_cny': format(cost, '.7f'), 'successful_decision_usd': format(decision, '.9f'),
                       'invalid_final_format': invalid, 'output_limit_stops': limited}
    return output


def load(root):
    raw = (root / 'snapshot.json').read_bytes()
    if hashlib.sha256(raw).hexdigest() != (root / 'snapshot.sha256').read_text().split()[0]:
        raise ValueError('Snapshot checksum mismatch')
    return json.loads(raw)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data-dir', type=Path, default=Path(__file__).resolve().parent)
    args = parser.parse_args()
    print(json.dumps(calculate(load(args.data_dir)), indent=2))
