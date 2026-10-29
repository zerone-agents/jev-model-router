"""Audit pinned Laya build_head using real Router fixtures, without loading weights.

The selected upstream functions are compiled unchanged; model imports are not
executed. Tokenization uses the uncached equivalent of _encode_question_text.
This is a preprocessing check, not a model inference or quality benchmark.
"""
import argparse
import ast
import hashlib
import json
import os
import tempfile
import warnings
from pathlib import Path
from typing import Dict, List, Optional

from transformers import AutoTokenizer


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--requests', required=True)
    p.add_argument('--model-dir', required=True)
    p.add_argument('--source', required=True)
    p.add_argument('--output', required=True)
    args = p.parse_args()
    source = Path(args.source).read_text()
    tree = ast.parse(source)
    names = {'render_criterion', 'render_options', 'build_head'}
    subset = ast.Module(body=[n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name in names], type_ignores=[])
    agent_source = Path(args.source).with_name('agent.py').read_text()
    fix = ast.Module(body=[n for n in ast.parse(agent_source).body if isinstance(n, ast.FunctionDef) and n.name == '_fix_tokenizer_config'], type_ignores=[])
    fix_env = {'os': os, 'json': json, 'tempfile': tempfile, 'warnings': warnings}
    exec(compile(fix, 'agent.py', 'exec'), fix_env)
    fix_env['_fix_tokenizer_config'](args.model_dir)
    tok = AutoTokenizer.from_pretrained(str(Path(args.model_dir)/'tokenizer'), local_files_only=True)
    env = {'Dict': Dict, 'List': List, 'Optional': Optional, 'json': json,
           '_encode_question_text': lambda tokenizer, text, **kw: tokenizer(text, **kw)['input_ids']}
    exec(compile(subset, args.source, 'exec'), env)
    cfg = json.loads((Path(args.model_dir)/'rl_agent_config.json').read_text())
    reports = []
    for name, request in json.loads(Path(args.requests).read_text()).items():
        for qid, qdef in request['questions'].items():
            q = {'t': qdef['type'], 'ins': qdef['instructions'], 'crit': qdef['criteria']}
            raw_instruction = tok('choice question: '+q['ins'], add_special_tokens=False)['input_ids']
            raw_options = [tok(' '+s, add_special_tokens=False)['input_ids'] for s in env['render_options'](q)]
            ids, markers, stats = env['build_head'](tok, q, cfg['head_max_len'])
            actual_instruction = ids[1:markers[0]-1]
            actual_options = [ids[m+1:(markers[i+1] if i+1<len(markers) else len(ids)-1)] for i,m in enumerate(markers)]
            state = request['state']
            state_text = state if isinstance(state,str) else json.dumps(state, ensure_ascii=False)
            state_ids = tok(state_text, add_special_tokens=False)['input_ids']
            retained = raw_instruction == actual_instruction and raw_options == actual_options
            reports.append({'fixture':name,'question':qid,'options':len(raw_options),
                'instruction_tokens':len(raw_instruction),'retained_instruction_tokens':len(actual_instruction),
                'option_tokens':[len(o) for o in raw_options], 'retained_option_tokens':[len(o) for o in actual_options],
                'uncapped_head_tokens':3+len(raw_instruction)+sum(1+len(o) for o in raw_options),
                'encoded_head_tokens':len(ids),'head_budget':cfg['head_max_len'],
                'state_tokens':len(state_ids),'fits_8192_context':len(ids)+len(state_ids)+1<=8192,
                'head_lossless':retained,'stats':stats})
    result={'preprocessing_only':True,'source_sha256':hashlib.sha256(source.encode()).hexdigest(),'results':reports}
    Path(args.output).write_text(json.dumps(result,indent=2)+'\n')
    for r in reports:
        print(f"{r['fixture']} {r['question']}: head={r['uncapped_head_tokens']} -> {r['encoded_head_tokens']}, instruction={r['instruction_tokens']} -> {r['retained_instruction_tokens']}, lossless={r['head_lossless']}")
    required=[r for r in reports if r['fixture'] in ('model-only','models-1','models-2')]
    raise SystemExit(0 if required and all(r['head_lossless'] and r['fits_8192_context'] for r in required) else 1)


if __name__ == '__main__':
    main()
