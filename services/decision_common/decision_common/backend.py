"""Execute independent System One questions with validated model inputs."""
import copy

from .protocol import ServiceError, parse_request


class DecisionBackend:
    def __init__(self, model, kind, alias, context):
        self.model = model
        self.kind = kind
        self.alias = alias
        self.context = context

    def __call__(self, payload):
        request = parse_request(payload, self.alias)
        questions = {q.name: {'type': 'choice', 'instructions': q.instructions,
                              'criteria': dict(q.criteria)} for q in request.questions}
        active = {key: value for key, value in questions.items() if len(value['criteria']) > 1}
        answers = {key: {'type': 'choice', 'choice': next(iter(value['criteria']))}
                   for key, value in questions.items() if len(value['criteria']) == 1}
        tokens = 0
        if active:
            if self.kind == 'd3':
                answers_active, tokens = self._d3(request.state, active)
            elif self.kind == 'decider':
                answers_active, tokens = self._decider(request.state, active)
            else:
                raise ServiceError('model_unavailable', 'Unknown model backend.', 503)
            if set(answers_active) != set(active):
                raise ServiceError('inference_error', 'Invalid model result.', 500)
            for key, result in answers_active.items():
                choice = result.get('choice')
                if choice not in active[key]['criteria']:
                    raise ServiceError('inference_error', 'Invalid model result.', 500)
                answers[key] = {'type': 'choice', 'choice': choice}
        return {'answers': answers, 'usage': {'input_tokens': tokens, 'output_tokens': 0}}

    def _d3(self, state, questions):
        runtime = self.model._require()
        prepared = runtime.prepare(state, questions)
        if prepared.errors:
            raise ServiceError('input_too_large', 'Input exceeds the model limit.', 413)
        if any(len(sequence) > self.context for sequence in prepared.sequences.values()):
            raise ServiceError('input_too_large', 'Input exceeds the context limit.', 413)
        probabilities, tokens = runtime.run(prepared)
        result = runtime.respond(prepared, probabilities, tokens)
        return result['answers'], tokens

    def _decider(self, state, questions):
        from decider.systemone import render_state, assemble
        from decider import temperature as temperatures
        tokenizer = self.model.m.tok
        rendered = render_state(state)
        if len(tokenizer.encode('Context:\n' + rendered, add_special_tokens=False)) > self.context:
            raise ServiceError('input_too_large', 'Input exceeds the context limit.', 413)
        rendered_questions, index, items = self.model._system_one_items(
            state, questions, independent=True, max_state_tokens=self.context)
        if any(len(item['ids']) > self.context for item in items):
            raise ServiceError('input_too_large', 'Input exceeds the context limit.', 413)
        for item, question in zip(items, questions.values()):
            if item['nopts'] != [len(question['criteria'])]:
                raise ServiceError('input_too_large', 'Input exceeds the option limit.', 413)
        frozen = copy.deepcopy(items)
        probabilities = self.model._system_one_probs(items, layout='state_first', max_fwd_tokens=self.context,
            temperature=temperatures.for_items(self.model.T, self.model.T_by_type, items))
        if items != frozen:
            raise ServiceError('inference_error', 'Model changed validated input.', 500)
        return assemble(rendered_questions, index, probabilities), sum(len(item['ids']) for item in items)
