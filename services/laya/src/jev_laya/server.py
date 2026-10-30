"""Uvicorn factory. Run exactly one ASGI worker; the supervisor owns model concurrency."""
from .app import create_app
from .config import Settings
from .runtime import ModelSettings, Runtime
from .worker import Supervisor


def application():
    settings = Settings.from_env()
    worker_settings = ModelSettings(settings.model_dir, settings.alias, settings.threads, settings.context, settings.head)
    supervisor = Supervisor(worker_settings, timeout=settings.deadline, factory=Runtime)
    return create_app(settings, supervisor)
