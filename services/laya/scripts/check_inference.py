"""Compare SDK inference with the same prevalidated, immutable encoded input.

Run only after check_feasibility succeeds. Uses synthetic Router fixtures;
CPU-only evidence is not production deployment or routing-quality acceptance.
"""
import argparse
import copy
import json
import os
import resource
import time

os.environ['HF_HUB_OFFLINE'] = '1'
os.environ['TRANSFORMERS_OFFLINE'] = '1'
os.environ['USE_TF'] = '0'

import laya
import torch
from laya.common import collate_items


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--requests', required=True)
    p.add_argument('--model-dir', required=True)
    p.add_argument('--output', required=True)
    a = p.parse_args()
    torch.set_num_threads(4)
    started = time.perf_counter()
    agent = laya.load(a.model_dir, device='cpu', backend='eager')
    load_seconds = time.perf_counter() - started
    reports = []
    for name, req in json.load(open(a.requests)).items():
        if name not in ('model-only', 'models-1', 'models-2', 'models-5'):
            continue
        qs = req['questions']
        ids = list(qs)
        internal = {k: agent._to_internal(v) for k, v in qs.items()}
        items = agent._encode_state(req['state'], ids, internal, max_len=8192, head_max_len=256)
        assert all(not x['state_stats']['truncated'] for x in items)
        frozen = copy.deepcopy(items)
        started = time.perf_counter()
        with torch.inference_mode():
            batch = collate_items([items], agent.tok.pad_token_id)
            logits, act = agent._forward(batch)
            answers = agent._decode_answers(logits, act, items, ids, internal, 0)
        prepared_seconds = time.perf_counter() - started
        assert items == frozen
        sdk = agent.predict(req['state'], qs, max_len=8192, head_max_len=256)
        assert answers == sdk['answers'], (answers, sdk['answers'])
        assert sdk['usage']['input_tokens'] == sum(len(x['ids']) for x in items)
        assert sdk['usage']['output_tokens'] == 0
        reports.append({'fixture': name, 'answers': answers, 'usage': sdk['usage'],
                        'prepared_seconds': prepared_seconds, 'sdk_equal': True})
        print(name, 'SDK equals prepared inference', flush=True)
    result = {'device':'cpu', 'threads':4, 'torch':torch.__version__, 'load_seconds':load_seconds,
              'maxrss_native_units':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss, 'results':reports}
    with open(a.output, 'w') as f:
        json.dump(result, f, indent=2)


if __name__ == '__main__':
    main()
