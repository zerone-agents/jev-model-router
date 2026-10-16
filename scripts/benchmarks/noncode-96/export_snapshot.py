#!/usr/bin/env python3
"""Export the frozen successful-response evidence; never modify the full archive."""
import argparse
import hashlib
import json
from pathlib import Path

ARCHIVE_MANIFEST = '9fab2c72ec90b75dd4a37b74f05311190a8bd72698385ab6212ffa829a222807'


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def export(source, output):
    source = source.resolve()
    if sha(source / 'sha256.json') != ARCHIVE_MANIFEST:
        raise ValueError('Unexpected source archive manifest')
    inventory = json.loads((source / 'sha256.json').read_text())
    for name, expected in inventory.items():
        path = (source / name).resolve()
        if not path.is_relative_to(source) or sha(path) != expected:
            raise ValueError('Source integrity failure: ' + name)
    evaluation = source / 'evaluation-recovered'
    tasks = json.loads((evaluation / 'tasks.json').read_text())
    records = {r['request_id']: r for r in json.loads((evaluation / 'routing-records.json').read_text())}
    answers = []
    for path in sorted((evaluation / 'results').glob('*.json')):
        if path.name.endswith('.started.json'):
            continue
        row = json.loads(path.read_text())
        if row['status'] != 200:
            raise ValueError('Snapshot must contain successful responses only')
        body = row['response']
        if row['job']['arm'] == 'semantic':
            text = body['choices'][0]['message']['content']
            actual = row['bridge_receipt']['response']
        else:
            text = ''.join(v.get('text', '') for v in body.get('content', []) if v.get('type') == 'text')
            actual = body
        answers.append({
            'task': row['job']['id'], 'arm': row['job']['arm'], 'status': row['status'],
            'model': actual['model'], 'final_line': text.strip().splitlines()[-1].strip() if text.strip() else '',
            'stop': actual['stop_reason'], 'usage': actual['usage'],
            'decision_usage': records[row['request_id']]['decision_usage'],
            'source_response_file': path.name, 'source_response_sha256': sha(path),
            'text_sha256': hashlib.sha256(text.encode()).hexdigest(),
        })
    runtime = json.loads((source / 'evaluation/runtime-provenance.json').read_text())
    runtime.pop('router_binary_path')
    data = {
        'schema_version': 1,
        'scope': 'Frozen non-code online pilot; selected successful responses after one service-failure-only recovery pass. Failed attempts excluded from reported costs.',
        'source_archive': source.name, 'source_manifest_sha256': ARCHIVE_MANIFEST,
        'verified_source_files': len(inventory),
        'source_files': json.loads((source / 'evaluation/source-provenance.json').read_text()),
        'runtime': runtime,
        'frozen_config_hashes': json.loads((source / 'frozen/freeze.json').read_text())['files'],
        'generation': json.loads((evaluation / 'manifest.json').read_text())['generation'],
        'recovery': {'initial_calls': 384, 'initial_service_failures': 17, 'additional_calls': 17, 'total_attempts': 401},
        'prices': json.loads((evaluation / 'prices.json').read_text()),
        'tasks': [{**{k: t[k] for k in ('id', 'kind', 'category', 'row', 'gold', 'question_sha256')},
                   'prompt_sha256': hashlib.sha256(t['prompt'].encode()).hexdigest()} for t in tasks],
        'answers': answers,
    }
    output.mkdir(parents=True, exist_ok=False)
    path = output / 'snapshot.json'
    path.write_text(json.dumps(data, indent=2, ensure_ascii=True) + '\n')
    (output / 'snapshot.sha256').write_text(sha(path) + '  snapshot.json\n')
    print(f'Exported {len(answers)} successful responses; verified {len(inventory)} source files')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True, help='New directory, must not exist')
    args = parser.parse_args()
    export(args.archive, args.output)
