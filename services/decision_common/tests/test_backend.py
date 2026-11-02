import json
import unittest
from types import SimpleNamespace

from decision_common.backend import DecisionBackend
from decision_common.protocol import ServiceError


def payload(criteria):
    return json.dumps({'model': 'test', 'state': 'hello', 'questions': {
        'model': {'type': 'choice', 'instructions': 'Choose', 'criteria': criteria}}}).encode()


class BackendTests(unittest.TestCase):
    def test_singleton_does_not_invoke_model(self):
        class Runtime:
            def prepare(self, state, questions):
                return SimpleNamespace(errors={}, sequences={'model': [1]}, reversed_sequences={})
            def run(self, prepared):
                raise AssertionError('singleton must not invoke inference')
        backend = DecisionBackend(SimpleNamespace(_require=lambda: Runtime()), 'd3', 'test', 100)
        result = backend(payload({'only': 'Only choice'}))
        self.assertEqual(result['answers']['model']['choice'], 'only')
        self.assertEqual(result['usage']['input_tokens'], 0)

    def test_d3_rejects_too_long_before_execution(self):
        class Runtime:
            def prepare(self, state, questions):
                return SimpleNamespace(errors={'model': {'error': 'max_length_exceeded'}}, sequences={})
            def run(self, prepared):
                raise AssertionError('must not execute rejected input')
        backend = DecisionBackend(SimpleNamespace(_require=lambda: Runtime()), 'd3', 'test', 100)
        with self.assertRaises(ServiceError) as caught:
            backend(payload({'a': 'A', 'b': 'B'}))
        self.assertEqual(caught.exception.code, 'input_too_large')

    def test_rejects_unknown_model_choice(self):
        backend = DecisionBackend(None, 'd3', 'test', 100)
        backend._d3 = lambda state, questions: ({'model': {'choice': 'other'}}, 1)
        with self.assertRaises(ServiceError):
            backend(payload({'a': 'A', 'b': 'B'}))

    def test_d3_rejects_oversized_reversed_sequence(self):
        class Runtime:
            def prepare(self, state, questions):
                return SimpleNamespace(errors={}, sequences={'model': [1, 2]},
                                       reversed_sequences={'model': [1] * 101})
            def run(self, prepared):
                raise AssertionError('must not execute rejected input')
        backend = DecisionBackend(SimpleNamespace(_require=lambda: Runtime()), 'd3', 'test', 100)
        with self.assertRaises(ServiceError) as caught:
            backend(payload({'a': 'A', 'b': 'B'}))
        self.assertEqual(caught.exception.code, 'input_too_large')

    def test_singleton_is_checked_before_bypass(self):
        class Runtime:
            def prepare(self, state, questions):
                return SimpleNamespace(errors={'model': {'error': 'max_length_exceeded'}}, sequences={}, reversed_sequences={})
        backend = DecisionBackend(SimpleNamespace(_require=lambda: Runtime()), 'd3', 'test', 100)
        with self.assertRaises(ServiceError) as caught:
            backend(payload({'only': 'long singleton'}))
        self.assertEqual(caught.exception.code, 'input_too_large')
