import json
import os
from pathlib import Path
import unittest

from decision_common.backend import DecisionBackend
from decision_common.protocol import parse_request


@unittest.skipUnless(os.environ.get('DECISION_TEST_MODEL') and os.environ.get('DECISION_TEST_FIXTURES'), 'requires pinned model and Router fixtures')
class RealModelTests(unittest.TestCase):
    def test_real_router_choices(self):
        kind = os.environ['DECISION_TEST_KIND']
        path = os.environ['DECISION_TEST_MODEL']
        device = os.environ.get('DECISION_TEST_DEVICE', 'cpu')
        if kind == 'decider':
            from decider.infer import Decider
            model = Decider(path, device=device, use_graphs=False)
        else:
            import torch
            from transformers import AutoModel
            model = AutoModel.from_pretrained(path, trust_remote_code=True,
                local_files_only=True, dtype=torch.bfloat16 if device == 'cuda' else torch.float32).to(device).eval()
        backend = DecisionBackend(model, kind, 'laya-multilingual', 8192)
        fixtures = json.loads(Path(os.environ['DECISION_TEST_FIXTURES']).read_text())
        for name in ('models-2', 'models-20', 'models-40'):
            with self.subTest(name=name):
                raw = fixtures[name]
                request = parse_request(json.dumps(raw).encode(), 'laya-multilingual')
                questions = {q.name: {'type': 'choice', 'instructions': q.instructions,
                                      'criteria': dict(q.criteria)} for q in request.questions}
                if kind == 'decider':
                    _, _, items = model._system_one_items(request.state, questions, independent=True, max_state_tokens=8192)
                    tokenizer = model.m.tok
                    sequences = {key: item['ids'] for key, item in zip(questions, items)}
                else:
                    runtime = model._require()
                    prepared = runtime.prepare(request.state, questions)
                    self.assertFalse(prepared.errors)
                    tokenizer = runtime.tokenizer
                    sequences = prepared.sequences
                for key, question in questions.items():
                    decoded = tokenizer.decode(sequences[key], skip_special_tokens=False)
                    self.assertIn(question['instructions'], decoded)
                    for candidate, description in question['criteria'].items():
                        self.assertIn(candidate + ': ' + description, decoded)
                result = backend(json.dumps(raw).encode())
                self.assertEqual(set(result['answers']), set(raw['questions']))
                for key, answer in result['answers'].items():
                    self.assertIn(answer['choice'], raw['questions'][key]['criteria'])
