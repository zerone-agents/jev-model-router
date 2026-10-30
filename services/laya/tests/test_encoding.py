import json
import os
from dataclasses import FrozenInstanceError
from pathlib import Path
import unittest

from jev_laya.encoding import prepare, Limits
from jev_laya.protocol import parse_request, ServiceError
from test_protocol import request


@unittest.skipUnless(os.environ.get('LAYA_MODEL_DIR'), 'requires pinned local tokenizer')
class EncodingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        from laya.agent import _fix_tokenizer_config
        _fix_tokenizer_config(os.environ['LAYA_MODEL_DIR'])
        from transformers import AutoTokenizer
        cls.tok = AutoTokenizer.from_pretrained(str(Path(os.environ['LAYA_MODEL_DIR'])/'tokenizer'), local_files_only=True)

    def prepare(self, value, limits=Limits()):
        return prepare(parse_request(json.dumps(value).encode(), 'laya-multilingual'), self.tok, limits)

    def test_prepared_input_is_immutable_and_preserves_cjk(self):
        result = self.prepare(request())
        self.assertIn('你好', self.tok.decode(result.questions[0].ids))
        with self.assertRaises(FrozenInstanceError): result.questions = ()
        with self.assertRaises(TypeError): result.questions[0].ids[0] = 9

    def test_context_exact_boundary_and_one_over(self):
        value = request()
        value['state'] = 'hello ' * 30
        full = self.prepare(value)
        size = len(full.questions[0].ids)
        self.prepare(value, Limits(context=size))
        with self.assertRaises(ServiceError) as caught:
            self.prepare(value, Limits(context=size-1))
        self.assertEqual(caught.exception.code, 'input_too_large')

    def test_head_and_individual_option_clipping_rejected(self):
        for target in ['instructions', 'option', 'many']:
            value = request()
            if target == 'instructions': value['questions']['model']['instructions'] = 'reason ' * 600
            if target == 'option': value['questions']['model']['criteria']['z'] = 'word ' * 60
            if target == 'many': value['questions']['model']['criteria'] = {str(i): 'Distinct option '+str(i) for i in range(64)}
            with self.subTest(target=target), self.assertRaises(ServiceError): self.prepare(value)

    def test_mask_replacement_not_silent(self):
        for place in ['state', 'instructions', 'description']:
            value = request()
            if place == 'state': value['state'] = 'secret '+self.tok.mask_token
            elif place == 'instructions': value['questions']['model']['instructions'] += self.tok.mask_token
            else: value['questions']['model']['criteria']['a'] += self.tok.mask_token
            with self.subTest(place=place), self.assertRaises(ServiceError): self.prepare(value)

    def test_all_questions_validated_before_singleton_bypass(self):
        value = request()
        value['questions']['model']['criteria'] = {'a': 'word '*70}
        with self.assertRaises(ServiceError): self.prepare(value)
