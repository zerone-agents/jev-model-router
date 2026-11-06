"""One authenticated System One worker for a pinned local decision model."""
from dataclasses import dataclass, field
import json
import os
from pathlib import Path

from .app import create_app
from .artifacts import verify_snapshot
from .backend import DecisionBackend
from .worker import Supervisor
from .variants import selected_manifest


@dataclass(frozen=True)
class Settings:
    token: str = field(repr=False)
    kind: str
    model_dir: str
    alias: str
    revision: str
    device: str
    threads: int
    context: int
    deadline: float


def read_settings():
    kind = os.environ['DECISION_BACKEND']
    if kind not in ('decider', 'd3'):
        raise ValueError('Unsupported decision backend')
    token = os.environ.get('DECISION_API_KEY', '')
    token_file = os.environ.get('DECISION_API_KEY_FILE')
    if token_file:
        if token:
            raise ValueError('Specify one decision token source')
        token = Path(token_file).read_text().strip()
    device = os.environ.get('DECISION_DEVICE', 'cpu')
    threads = int(os.environ.get('DECISION_CPU_THREADS', '4'))
    context = int(os.environ.get('DECISION_CONTEXT_TOKENS', '8192'))
    deadline = float(os.environ.get('DECISION_WORKER_TIMEOUT_SECONDS', '60'))
    if not token or device not in ('cpu', 'cuda') or not 1 <= threads <= 64 or not 1 <= context <= 32768 or not 0 < deadline < float('inf'):
        raise ValueError('Invalid decision service settings')
    manifest_path, expected_model = selected_manifest(kind)
    manifest = json.loads(manifest_path.read_text())
    if manifest.get('model') != expected_model:
        raise ValueError('Manifest does not match the decision backend')
    alias = os.environ.get('DECISION_MODEL_ALIAS', kind)
    if not alias:
        raise ValueError('Model alias is required')
    return Settings(token, kind, os.environ['DECISION_MODEL_DIR'], alias,
                    manifest['model_revision'], device, threads, context, deadline)


class Runtime:
    def __init__(self, settings):
        os.environ['HF_HUB_OFFLINE'] = '1'
        os.environ['TRANSFORMERS_OFFLINE'] = '1'
        os.environ['USE_TF'] = '0'
        manifest_path, expected_model = selected_manifest(settings.kind)
        manifest = json.loads(manifest_path.read_text())
        if manifest['model_revision'] != settings.revision or manifest.get('model') != expected_model:
            raise ValueError('Model revision changed')
        snapshot = str(Path(settings.model_dir).resolve(strict=True))
        verify_snapshot(snapshot, manifest)
        import torch
        torch.set_num_threads(settings.threads)
        if settings.device == 'cuda' and not torch.cuda.is_available():
            raise ValueError('Requested CUDA device is unavailable')
        if settings.device == 'cpu':
            from .cpu import prepare_cpu_kernels
            prepare_cpu_kernels()
        if settings.kind == 'decider':
            from decider.infer import Decider
            model = Decider(snapshot, device=settings.device, use_graphs=False)
        else:
            from transformers import AutoModel
            model = AutoModel.from_pretrained(snapshot, trust_remote_code=True,
                local_files_only=True, dtype='auto').to(settings.device).eval()
        self.backend = DecisionBackend(model, settings.kind, settings.alias, settings.context)
        smoke = {'model': settings.alias, 'state': 'hello', 'questions': {'q': {
            'type': 'choice', 'instructions': 'Choose a greeting', 'criteria': {'a': 'hello', 'b': 'goodbye'}}}}
        self.backend(json.dumps(smoke).encode())

    def __call__(self, payload):
        return self.backend(payload)


def application():
    settings = read_settings()
    return create_app(settings, Supervisor(settings, timeout=settings.deadline, factory=Runtime))
