import unittest
from jev_laya.encoding import PreparedDecision, EncodedQuestion
from jev_laya.model import evaluate
from jev_laya.protocol import ServiceError


class ModelTests(unittest.TestCase):
    def test_usage_counts_processed_positions_and_singleton_is_zero(self):
        many = EncodedQuestion('model', ('a','b'), (1,2,3), (1,2), (), ())
        single = EncodedQuestion('effort', ('low',), (1,2), (1,), (), ())
        def backend(items):
            self.assertEqual([q.name for q in items], ['model'])
            return {'model': 'b'}
        result = evaluate(PreparedDecision((many,single)), backend)
        self.assertEqual(result, {'answers': {'model': {'type':'choice','choice':'b'}, 'effort': {'type':'choice','choice':'low'}}, 'usage': {'input_tokens':3,'output_tokens':0}})

    def test_invalid_backend_results_fail_atomically(self):
        prepared = PreparedDecision((EncodedQuestion('model', ('a','b'), (1,2,3), (1,2), (), ()),))
        for answer in [{}, {'model':'other'}, {'model':'a','extra':'b'}, {'model':float('nan')}, None]:
            with self.subTest(answer=answer), self.assertRaises(ServiceError):
                evaluate(prepared, lambda _: answer)

    def test_nonfinite_logits_cannot_become_a_valid_choice(self):
        import numpy as np
        from types import SimpleNamespace
        from jev_laya.model import LayaBackend
        backend = LayaBackend.__new__(LayaBackend)
        backend.tokenizer = SimpleNamespace(pad_token_id=0)
        backend.agent = SimpleNamespace(parallel_options=False,
            _forward=lambda _: (np.array([[float('nan'), 0.]]), np.array([[1.]])),
            _decode_answers=lambda *args: {'model': {'choice': 'a'}})
        question = EncodedQuestion('model', ('a','b'), (1,2,3), (1,2), (), ())
        with self.assertRaises(ServiceError): backend((question,))
