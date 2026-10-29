import copy
import json
import unittest

from jev_laya.protocol import parse_request, ServiceError


def request():
    return {'model': 'laya-multilingual', 'state': {'text': '你好'}, 'questions': {
        'model': {'type': 'choice', 'instructions': 'Choose.', 'criteria': {'z': 'Fast', 'a': 'Strong'}}}}


class ProtocolTests(unittest.TestCase):
    def parse(self, value):
        return parse_request(json.dumps(value).encode(), 'laya-multilingual')

    def test_order_and_singleton_preserved(self):
        result = self.parse(request())
        self.assertEqual(result.questions[0].criteria, (('z', 'Fast'), ('a', 'Strong')))
        value = request()
        value['questions']['model']['criteria'] = {'only': 'Single'}
        self.assertEqual(self.parse(value).questions[0].criteria, (('only', 'Single'),))

    def test_second_question_supported_third_rejected(self):
        value = request()
        value['questions']['effort'] = copy.deepcopy(value['questions']['model'])
        self.assertEqual(len(self.parse(value).questions), 2)
        value['questions']['third'] = value['questions']['model']
        with self.assertRaises(ServiceError): self.parse(value)

    def test_invalid_json_and_nonfinite_never_accepted(self):
        for raw in [b'\xff', b'{', b'{"model": "x", "model": "y"}',
                    b'{"state":NaN}', b'{"state":1e999}', b'[]', b'null']:
            with self.subTest(raw=raw), self.assertRaises(ServiceError):
                parse_request(raw, 'laya-multilingual')

    def test_duplicate_nested_keys_rejected(self):
        raw = json.dumps(request()).replace('"z": "Fast"', '"z": "Fast", "z": "Other"')
        with self.assertRaises(ServiceError): parse_request(raw.encode(), 'laya-multilingual')

    def test_unknown_and_invalid_fields(self):
        for change in [lambda r: r.update(extra=True), lambda r: r.update(model='other'),
                       lambda r: r.update(state=[]), lambda r: r['questions']['model'].update(type='rating'),
                       lambda r: r['questions']['model'].update(extra=True),
                       lambda r: r['questions']['model'].update(criteria={}),
                       lambda r: r['questions']['model'].update(criteria={'a': 7}),
                       lambda r: r['questions']['model'].update(criteria={str(i): 'x' for i in range(65)}),
                       lambda r: r['questions']['model'].update(instructions=''),
                       lambda r: r.update(state='\ud800')]:
            value = request(); change(value)
            with self.subTest(value=value), self.assertRaises(ServiceError): self.parse(value)

    def test_body_limit(self):
        with self.assertRaises(ServiceError) as error:
            parse_request(b' ' * 65537, 'laya-multilingual')
        self.assertEqual(error.exception.code, 'input_too_large')
