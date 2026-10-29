import json
import os
import unittest

from jev_laya.encoding import prepare
from jev_laya.model import LayaBackend, evaluate
from jev_laya.protocol import parse_request


@unittest.skipUnless(os.environ.get('LAYA_REAL_MODEL') and os.environ.get('LAYA_FIXTURES'), 'requires real checkpoint and Go fixtures')
class RealModelTests(unittest.TestCase):
    def test_router_requests_match_sdk_and_usage(self):
        backend = LayaBackend(os.environ['LAYA_REAL_MODEL'])
        with open(os.environ['LAYA_FIXTURES']) as f:
            fixtures = json.load(f)
        for name in ['model-only','models-1','models-2','models-5']:
            with self.subTest(name=name):
                raw = fixtures[name]
                request = parse_request(json.dumps(raw).encode(), 'laya-multilingual')
                prepared = prepare(request, backend.tokenizer)
                result = evaluate(prepared, backend)
                expected = backend.agent.predict(raw['state'], raw['questions'], max_len=8192, head_max_len=256)
                self.assertEqual(result['usage']['input_tokens'], expected['usage']['input_tokens'])
                self.assertEqual(result['usage']['output_tokens'], 0)
                self.assertEqual(result['answers'], {k: {'type':'choice','choice':v['choice']} for k,v in expected['answers'].items()})
                for q in prepared.questions:
                    self.assertIsInstance(q.ids, tuple)
        for name in ['models-20', 'models-40']:
            from jev_laya.protocol import ServiceError
            with self.subTest(name=name), self.assertRaises(ServiceError):
                prepare(parse_request(json.dumps(fixtures[name]).encode(), 'laya-multilingual'), backend.tokenizer)
