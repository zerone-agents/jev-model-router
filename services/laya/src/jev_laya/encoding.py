"""Immutable exact inputs for the pinned SDK; never silently truncate."""
from dataclasses import dataclass
from .protocol import DecisionRequest, ServiceError


@dataclass(frozen=True)
class Limits:
    context: int = 8192
    head: int = 512

    def __post_init__(self):
        if not 1 <= self.context <= 8192 or not 1 <= self.head <= 8188:
            raise ValueError('Unsupported model token limits')


@dataclass(frozen=True)
class EncodedQuestion:
    name: str
    choices: tuple[str, ...]
    ids: tuple[int, ...]
    markers: tuple[int, ...]
    position_ids: tuple[int, ...]
    option_ids: tuple[int, ...]


@dataclass(frozen=True)
class PreparedDecision:
    questions: tuple[EncodedQuestion, ...]


def oversized(limit):
    return ServiceError('input_too_large', f'Input exceeds the {limit} token budget.', 413)


def prepare(request: DecisionRequest, tokenizer, limits: Limits = Limits()) -> PreparedDecision:
    from laya.common import build_head, render_options, parallel_layout

    def encode(text):
        if tokenizer.mask_token in text:
            raise ServiceError('invalid_request', 'Literal mask control tokens are unsupported.')
        return tokenizer(text, add_special_tokens=False, truncation=False)['input_ids']

    state = encode(request.state)
    prepared = []
    for question in request.questions:
        internal = question.internal()
        instruction = encode('choice question: ' + question.instructions)
        options = [encode(' ' + text) for text in render_options(internal)]
        if any(len(option) > 48 for option in options):
            raise oversized('per-option (48)')
        # Verify each original segment against SDK formatting. Marker survival alone
        # cannot detect clipped instructions or options with an identical prefix.
        head = [tokenizer.cls_token_id] + instruction + [tokenizer.sep_token_id]
        markers = []
        for option in options:
            markers.append(len(head))
            head += [tokenizer.mask_token_id] + option
        head.append(tokenizer.sep_token_id)
        if len(head) - 3 > limits.head:
            raise oversized(f'head ({limits.head})')
        sdk_head, sdk_markers, _ = build_head(tokenizer, internal, limits.head)
        if head != sdk_head or markers != sdk_markers:
            raise oversized('SDK head allocation')
        ids = head + state + [tokenizer.sep_token_id]
        if len(ids) > limits.context:
            raise oversized('context')
        layout = parallel_layout(markers, len(head), len(ids))
        prepared.append(EncodedQuestion(question.name, tuple(k for k, _ in question.criteria),
                                       tuple(ids), tuple(markers), tuple(layout['position_ids']),
                                       tuple(layout['option_ids'])))
    return PreparedDecision(tuple(prepared))
