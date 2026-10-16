import copy
import json
from pathlib import Path
import tempfile
import unittest

import reproduce

ROOT = Path(__file__).resolve().parent


class ReproductionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = reproduce.load(ROOT)

    def test_matches_independent_archived_summary(self):
        self.assertEqual(reproduce.calculate(self.data), json.loads((ROOT / 'expected.json').read_text()))

    def test_frozen_parser(self):
        self.assertEqual(reproduce.grade('FINAL: -0.50', 'math', '-.5'), (True, True))
        self.assertEqual(reproduce.grade('FINAL: false', 'reading', 'false'), (True, True))
        self.assertEqual(reproduce.grade('FINAL: true', 'reading', 'false'), (False, True))
        for line in ('FINAL: A or B', 'FINAL: A.', 'The answer is A'):
            self.assertEqual(reproduce.grade(line, 'knowledge', 'A'), (False, False))
        self.assertEqual(reproduce.grade('FINAL: 1,000', 'math', '1000'), (False, False))

    def test_missing_or_duplicate_response_is_rejected(self):
        for duplicate in (False, True):
            data = copy.deepcopy(self.data)
            data['answers'].pop()
            if duplicate:
                data['answers'].append(data['answers'][0])
            with self.assertRaises(ValueError):
                reproduce.calculate(data)

    def test_failed_attempt_cannot_enter_successful_snapshot(self):
        data = copy.deepcopy(self.data)
        data['answers'][0]['status'] = 500
        with self.assertRaises(ValueError):
            reproduce.calculate(data)

    def test_unknown_usage_is_not_zero(self):
        data = copy.deepcopy(self.data)
        data['answers'][0]['usage']['output_tokens'] = None
        with self.assertRaises(ValueError):
            reproduce.calculate(data)
        data = copy.deepcopy(self.data)
        data['answers'][0]['decision_usage']['complete'] = False
        with self.assertRaises(ValueError):
            reproduce.calculate(data)

    def test_checksum_detects_modified_evidence(self):
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            (directory / 'snapshot.json').write_bytes((ROOT / 'snapshot.json').read_bytes() + b' ')
            (directory / 'snapshot.sha256').write_bytes((ROOT / 'snapshot.sha256').read_bytes())
            with self.assertRaises(ValueError):
                reproduce.load(directory)


if __name__ == '__main__':
    unittest.main()
