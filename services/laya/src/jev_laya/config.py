"""Environment-only service configuration; token values never enter metadata."""
from dataclasses import dataclass, field
import math
import os
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    token: str = field(repr=False)
    model_dir: str
    alias: str = 'laya-multilingual'
    threads: int = 4
    context: int = 8192
    deadline: float = 60.0

    def __post_init__(self):
        if not self.token or not self.alias or not self.model_dir:
            raise ValueError('Service token, alias and model directory are required')
        if not 1 <= self.threads <= 64 or not 1 <= self.context <= 8192:
            raise ValueError('Invalid model limits')
        if not math.isfinite(self.deadline) or self.deadline <= 0:
            raise ValueError('Invalid worker deadline')

    @classmethod
    def from_env(cls):
        token = os.environ.get('LAYA_API_KEY', '')
        path = os.environ.get('LAYA_API_KEY_FILE')
        if path:
            if token: raise ValueError('Use either token environment or token file')
            token = Path(path).read_text().strip()
        return cls(token=token, model_dir=os.environ.get('LAYA_MODEL_DIR', '/models/current'),
                   alias=os.environ.get('LAYA_MODEL_ALIAS', 'laya-multilingual'),
                   threads=int(os.environ.get('LAYA_CPU_THREADS', '4')),
                   context=int(os.environ.get('LAYA_CONTEXT_TOKENS', '8192')),
                   deadline=float(os.environ.get('LAYA_WORKER_TIMEOUT_SECONDS', '60')))
