"""Offline model lifetime; writable SDK normalization is isolated from verified weights."""
from dataclasses import dataclass
from pathlib import Path
import json
import shutil
import tempfile

from .encoding import prepare, Limits
from .model import LayaBackend, evaluate
from .protocol import parse_request
from .provision import load_manifest, verify


@dataclass(frozen=True)
class ModelSettings:
    model_dir: str
    alias: str
    threads: int
    context: int


class Runtime:
    def __init__(self, settings: ModelSettings):
        self.settings = settings
        manifest = load_manifest()
        verify(settings.model_dir, manifest)
        # The SDK rewrites tokenizer_config.json. Never mutate verified artifacts
        # or require the read-only deployment volume to become writable.
        self._temporary = tempfile.TemporaryDirectory(prefix='jev-laya-')
        target = Path(self._temporary.name)
        source = Path(settings.model_dir).resolve()
        for entry in manifest['artifacts']:
            name = entry['path']
            path = target / name
            path.parent.mkdir(parents=True, exist_ok=True)
            if name == 'tokenizer/tokenizer_config.json': shutil.copyfile(source/name, path)
            else: path.symlink_to(source/name)
        self.backend = LayaBackend(str(target), settings.threads)
        smoke = {'model': settings.alias, 'state': 'hello', 'questions': {'startup': {'type': 'choice', 'instructions': 'Select a greeting.', 'criteria': {'a': 'Hello', 'b': 'Goodbye'}}}}
        self(json.dumps(smoke).encode())

    def __call__(self, body):
        request = parse_request(body, self.settings.alias)
        prepared = prepare(request, self.backend.tokenizer, Limits(context=self.settings.context))
        return evaluate(prepared, self.backend)
