#!/usr/bin/env python3
"""Evaluate the frozen synthetic 128-case set against a loopback System One service.

PRIVACY_EVAL_KEY=... PRIVACY_EVAL_MODEL=... python3 evals/privacy_evaluate_128.py MODEL PORT PROMPT_NAME
Set PRIVACY_EVAL_SPLIT to previously_seen or new_holdout to evaluate only that split.
Only synthetic data is sent. Output contains labels and outcomes, never credentials.
"""
import collections
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.request

PROMPTS = {
    'original': 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.',
    'preview': 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, medical information, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.',
    'round1_three': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                     'Example 1: Private employee medical and compensation records. Decision: sensitive.\n'
                     'Example 2: 内部客户合同中的保密价格和测试凭证。 Decision: sensitive.\n'
                     'Example 3: Explain how an ordinary public website works. Decision: non_sensitive.'),
    'round2_four': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                    'Example 1: Private employee medical and compensation records. Decision: sensitive.\n'
                    'Example 2: 内部客户合同中的保密价格和测试凭证。 Decision: sensitive.\n'
                    'Example 3: Private bank account transactions. Decision: sensitive.\n'
                    'Example 4: Explain how an ordinary public website works. Decision: non_sensitive.'),
    'round3_four': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                    'Example 1: Private employee medical and compensation records. Decision: sensitive.\n'
                    'Example 2: 内部客户合同中的保密价格和测试凭证。 Decision: sensitive.\n'
                    'Example 3: Explain how an ordinary public website works. Decision: non_sensitive.\n'
                    'Example 4: 解释通用的发票字段，不包含真实客户资料。 Decision: non_sensitive.'),
    'round4_five': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                    'Example 1: Private employee medical and compensation records. Decision: sensitive.\n'
                    'Example 2: 内部客户合同中的保密价格和测试凭证。 Decision: sensitive.\n'
                    'Example 3: Private bank account transactions. Decision: sensitive.\n'
                    'Example 4: Explain how an ordinary public website works. Decision: non_sensitive.\n'
                    'Example 5: 解释通用的发票字段，不包含真实客户资料。 Decision: non_sensitive.'),
    'five_examples': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                      'Example 1: A private personnel file contains a staff member’s medical leave and compensation. Decision: sensitive.\n'
                      'Example 2: An internal sales document contains a confidential customer quote and a test service credential. Decision: sensitive.\n'
                      'Example 3: A private customer file contains an account number and transaction details. Decision: sensitive.\n'
                      'Example 4: Explain how an ordinary public website works. Decision: non_sensitive.\n'
                      'Example 5: Describe the fields in a generic invoice without any real data. Decision: non_sensitive.'),
    'three_examples': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                       'Example 1: A private personnel file contains a staff member’s medical leave and compensation. Decision: sensitive.\n'
                       'Example 2: An internal sales document contains a confidential customer quote and a test service credential. Decision: sensitive.\n'
                       'Example 3: Explain how an ordinary public website works. Decision: non_sensitive.'),
    'three_bilingual': ('Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.\n'
                        'Example 1: A private customer record contains medical and salary details. Decision: sensitive.\n'
                        'Example 2: 内部客户合同和测试账号密钥。 Decision: sensitive.\n'
                        'Example 3: Explain how a public website works. Decision: non_sensitive.'),
    'oneshot_employee': 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure. Example request: An employee asks to summarize their private treatment record and annual compensation. Decision: sensitive.',
    'oneshot_business': 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure. Example request: An internal customer contract contains a confidential price and a test access credential. Decision: sensitive.',
    'oneshot_mixed': 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure. Example request: A private employee record includes medical information, a customer quote and a test account password. Decision: sensitive.',
}

def main():
    if len(sys.argv) != 4 or sys.argv[3] not in PROMPTS or not sys.argv[2].isdigit():
        raise SystemExit('Usage: privacy_evaluate_128.py MODEL PORT PROMPT_NAME')
    model, port, prompt_name = sys.argv[1:]
    api_key = os.environ['PRIVACY_EVAL_KEY']
    model_alias = os.environ.get('PRIVACY_EVAL_MODEL', 'laya-multilingual')
    dataset = json.loads((Path(__file__).parent / 'privacy_cases_128.json').read_text())['cases']
    if len(dataset) != 128 or len({case['id'] for case in dataset}) != 128:
        raise SystemExit('Invalid dataset: expected 128 unique cases')
    split = os.environ.get('PRIVACY_EVAL_SPLIT', 'all')
    if split not in ('all', 'previously_seen', 'new_holdout'):
        raise SystemExit('Invalid PRIVACY_EVAL_SPLIT')
    if split != 'all':
        dataset = [case for case in dataset if case['split'] == split]
    results = []
    start = time.monotonic()
    for case in dataset:
        body = {'model': model_alias, 'state': {'model': 'auto', 'messages': case['messages']},
                'questions': {'privacy': {'type': 'choice', 'instructions': PROMPTS[prompt_name],
                'criteria': {'non_sensitive': 'No sensitive information is present.',
                             'sensitive': 'Sensitive or confidential information is present.',
                             'uncertain': 'The sensitivity cannot be determined reliably.'}}}}
        request = urllib.request.Request('http://127.0.0.1:' + port + '/v1/systemone',
                  data=json.dumps(body, ensure_ascii=False).encode(),
                  headers={'Authorization': 'Bearer ' + api_key, 'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                obj = json.load(response)
                result = obj.get('answers', {}).get('privacy', {}).get('choice', 'invalid')
                status = response.status
        except urllib.error.HTTPError as error:
            result, status = 'error', error.code
        except Exception as error:
            result, status = 'error', type(error).__name__
        if result not in ('sensitive', 'non_sensitive', 'uncertain', 'error'):
            result = 'invalid'
        results.append({'id': case['id'], 'split': case['split'], 'label': case['label'],
                        'language': case['language'], 'category': case.get('category', 'previous'),
                        'outcome': result, 'status': status})
    def summary(rows):
        return {'total': len(rows),
                'sensitive': sum(r['label'] == 'sensitive' for r in rows),
                'public': sum(r['label'] == 'public' for r in rows),
                'false_negative': sum(r['label'] == 'sensitive' and r['outcome'] == 'non_sensitive' for r in rows),
                'false_positive': sum(r['label'] == 'public' and r['outcome'] == 'sensitive' for r in rows),
                'uncertain': sum(r['outcome'] == 'uncertain' for r in rows),
                'errors': sum(r['outcome'] in ('error', 'invalid') for r in rows),
                'error_statuses': dict(collections.Counter(str(r['status']) for r in rows if r['outcome'] in ('error', 'invalid')))}
    print(json.dumps({'model': model, 'prompt': prompt_name, 'elapsed_seconds': round(time.monotonic()-start, 3),
                      'overall': summary(results), 'new_holdout': summary([r for r in results if r['split'] == 'new_holdout']),
                      'previously_seen': summary([r for r in results if r['split'] == 'previously_seen']),
                      'cases': results}, ensure_ascii=False))

if __name__ == '__main__':
    main()
