"""Small pinned-SDK boundary. Execute already validated encodings exactly once."""
from .encoding import PreparedDecision
from .protocol import ServiceError


def evaluate(prepared: PreparedDecision, backend) -> dict:
    active = tuple(q for q in prepared.questions if len(q.choices) > 1)
    choices = backend(active) if active else {}
    if not isinstance(choices, dict) or set(choices) != {q.name for q in active}:
        raise ServiceError('inference_error', 'Invalid model result.', 500)
    answers = {}
    for question in prepared.questions:
        choice = question.choices[0] if len(question.choices) == 1 else choices[question.name]
        if not isinstance(choice, str) or choice not in question.choices:
            raise ServiceError('inference_error', 'Invalid model result.', 500)
        answers[question.name] = {'type': 'choice', 'choice': choice}
    return {'answers': answers, 'usage': {'input_tokens': sum(len(q.ids) for q in active), 'output_tokens': 0}}


class LayaBackend:
    def __init__(self, model_dir: str, threads: int = 4, device: str = 'cpu'):
        import os
        os.environ['HF_HUB_OFFLINE'] = '1'
        os.environ['TRANSFORMERS_OFFLINE'] = '1'
        os.environ['USE_TF'] = '0'
        import laya
        import torch
        torch.set_num_threads(threads)
        if device not in ('cpu', 'mps'):
            raise ValueError('Unsupported model device')
        if device == 'mps' and not torch.backends.mps.is_available():
            raise RuntimeError('Requested MPS device is unavailable')
        self.agent = laya.load(model_dir, device=device, backend='eager')
        if self.agent.device.type != device:
            raise RuntimeError('Requested model device was not selected')
        self.tokenizer = self.agent.tok

    def __call__(self, questions):
        import torch
        from laya.common import collate_items, QTYPES
        items = []
        for q in questions:
            item = {'ids': list(q.ids), 'markers': list(q.markers), 'qtype': QTYPES['choice']}
            if self.agent.parallel_options:
                item['layout'] = {'position_ids': list(q.position_ids), 'option_ids': list(q.option_ids)}
            items.append(item)
        with torch.inference_mode():
            logits, activity = self.agent._forward(collate_items([items], self.tokenizer.pad_token_id))
            import numpy as np
            if any(not np.isfinite(logits[i, :len(q.choices)]).all() for i, q in enumerate(questions)) or not np.isfinite(activity).all():
                raise ServiceError('inference_error', 'Invalid model result.', 500)
            # Use the SDK decoder to retain its precise masked choice semantics.
            internal = {q.name: {'t': 'choice', 'ins': '', 'crit': dict.fromkeys(q.choices, '')} for q in questions}
            answers = self.agent._decode_answers(logits, activity, items, [q.name for q in questions], internal, 0)
        return {name: answer['choice'] for name, answer in answers.items()}
