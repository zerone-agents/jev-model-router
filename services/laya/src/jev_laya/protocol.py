"""Strict System One choice subset. No user text appears in raised errors."""
from dataclasses import dataclass
import json
import math

BODY_LIMIT = 65_536


class ServiceError(Exception):
    def __init__(self, code: str, message: str, status: int = 400):
        super().__init__(message)
        self.code, self.message, self.status = code, message, status


@dataclass(frozen=True)
class Question:
    name: str
    instructions: str
    criteria: tuple[tuple[str, str], ...]

    def internal(self):
        return {'t': 'choice', 'ins': self.instructions, 'crit': dict(self.criteria)}


@dataclass(frozen=True)
class DecisionRequest:
    model: str
    state: str
    questions: tuple[Question, ...]


def invalid():
    return ServiceError('invalid_request', 'Invalid System One choice request.')


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise invalid()
        result[key] = value
    return result


def _finite(value):
    if isinstance(value, float) and not math.isfinite(value):
        raise invalid()
    if isinstance(value, str):
        value.encode('utf-8', errors='strict')
    elif isinstance(value, dict):
        for key, item in value.items():
            _finite(key)
            _finite(item)
    elif isinstance(value, list):
        for item in value:
            _finite(item)


def _text(value):
    return isinstance(value, str) and bool(value.strip())


def parse_request(body: bytes, alias: str) -> DecisionRequest:
    if len(body) > BODY_LIMIT:
        raise ServiceError('input_too_large', 'HTTP body exceeds 65536 bytes.', 413)
    try:
        value = json.loads(body.decode('utf-8'), object_pairs_hook=_object)
        _finite(value)
        if not isinstance(value, dict) or set(value) != {'model', 'state', 'questions'}:
            raise invalid()
        if value['model'] != alias:
            raise ServiceError('unknown_model', 'Only the configured model alias is supported.')
        if not isinstance(value['state'], (dict, str)):
            raise invalid()
        questions = value['questions']
        if not isinstance(questions, dict) or not 1 <= len(questions) <= 2:
            raise invalid()
        parsed = []
        for name, question in questions.items():
            if not _text(name) or not isinstance(question, dict):
                raise invalid()
            if set(question) != {'type', 'instructions', 'criteria'}:
                raise invalid()
            if question['type'] != 'choice':
                raise ServiceError('unsupported_question', 'Only choice questions are supported.')
            criteria = question['criteria']
            if not _text(question['instructions']) or not isinstance(criteria, dict) or not 1 <= len(criteria) <= 64:
                raise invalid()
            if any(not _text(k) or not _text(v) for k, v in criteria.items()):
                raise invalid()
            parsed.append(Question(name, question['instructions'], tuple(criteria.items())))
        state = value['state']
        if isinstance(state, dict):
            state = json.dumps(state, ensure_ascii=False)
        return DecisionRequest(alias, state, tuple(parsed))
    except (ValueError, UnicodeError, RecursionError, OverflowError):
        raise invalid() from None
