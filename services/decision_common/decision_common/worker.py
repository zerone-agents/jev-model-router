"""One supervised model process, no queue, cancellation-safe slot ownership."""
import asyncio
import contextlib
import multiprocessing
import os
import time

from .protocol import ServiceError


def _worker(connection, factory, settings):
    # Third-party libraries must not print request data or tracebacks to logs.
    with open(os.devnull, 'w') as sink, contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
        try:
            evaluator = factory(settings)
            connection.send(('ready', None))
            while True:
                payload = connection.recv()
                try:
                    result = evaluator(payload)
                    connection.send(('result', result))
                except ServiceError as error:
                    connection.send(('error', (error.code, error.message, error.status)))
                except Exception:
                    connection.send(('error', ('inference_error', 'Model inference failed.', 500)))
        except (EOFError, BrokenPipeError):
            pass
        except Exception:
            with contextlib.suppress(Exception):
                connection.send(('error', ('model_unavailable', 'Model initialization failed.', 503)))
        finally:
            connection.close()


class Supervisor:
    def __init__(self, settings, timeout=60.0, *, factory, startup_timeout=120.0):
        if timeout <= 0 or startup_timeout <= 0:
            raise ValueError('Worker timeouts must be positive')
        self.settings, self.timeout, self.factory = settings, timeout, factory
        self.startup_timeout = startup_timeout
        self._process = None
        self._pipe = None
        self._ready = False
        self._closed = False
        self._owner = None
        self._loader = None
        self._watcher = None

    def start(self):
        if not self._closed and self._watcher is None:
            self._watcher = asyncio.create_task(self._watch_idle())
        if not self._closed and self._loader is None:
            self._loader = asyncio.create_task(self._load())

    async def _watch_idle(self):
        while not self._closed:
            await asyncio.sleep(0.05)
            if (self._owner is None and self._loader is None and self._process is not None
                    and not self._process.is_alive()):
                await self._reap()
                self.start()

    def ready(self):
        return bool(self._ready and not self._closed and self._process and self._process.is_alive())

    async def _receive(self, deadline):
        while time.monotonic() < deadline and not self._closed:
            if self._pipe.poll():
                return self._pipe.recv()
            if not self._process.is_alive():
                break
            await asyncio.sleep(0.01)
        raise ServiceError('model_unavailable', 'Model worker unavailable or deadline exceeded.', 503)

    async def _reap(self):
        self._ready = False
        process, pipe = self._process, self._pipe
        self._process = self._pipe = None
        if process is not None:
            if process.is_alive(): process.terminate()
            await asyncio.to_thread(process.join, 0.5)
            if process.is_alive():
                process.kill()
                await asyncio.to_thread(process.join, 0.5)
            if process.is_alive():
                self._closed = True
                raise RuntimeError('Could not stop model worker')
            process.close()
        if pipe is not None: pipe.close()

    async def _load(self):
        try:
            ctx = multiprocessing.get_context('spawn')
            self._pipe, child = ctx.Pipe()
            self._process = ctx.Process(target=_worker, args=(child, self.factory, self.settings), daemon=True)
            self._process.start()
            child.close()
            kind, _ = await self._receive(time.monotonic() + self.startup_timeout)
            if kind != 'ready':
                raise ServiceError('model_unavailable', 'Model initialization failed.', 503)
            self._ready = True
        except asyncio.CancelledError:
            raise
        except Exception:
            await self._reap()
            # Failed startup remains unavailable until process restart. Do not loop
            # downloads/reloads indefinitely for a missing or corrupt checkpoint.
        finally:
            self._loader = None

    async def submit(self, payload):
        if self._owner is not None:
            raise ServiceError('overloaded', 'Model worker is busy; retry later.', 429)
        if not self.ready():
            raise ServiceError('model_unavailable', 'Model worker is not ready.', 503)
        self._owner = asyncio.create_task(self._execute(payload))
        # The task retains the slot after the HTTP caller is canceled.
        self._owner.add_done_callback(_consume_exception)
        return await asyncio.shield(self._owner)

    async def _execute(self, payload):
        try:
            self._pipe.send(payload)
            kind, value = await self._receive(time.monotonic() + self.timeout)
            if kind == 'error': raise ServiceError(*value)
            if kind != 'result': raise RuntimeError('Invalid worker response')
            return value
        except ServiceError as error:
            if error.code == 'model_unavailable':
                await self._reap()
                if not self._closed: self.start()
            raise
        except (EOFError, BrokenPipeError, OSError):
            await self._reap()
            if not self._closed: self.start()
            raise ServiceError('model_unavailable', 'Model worker stopped.', 503) from None
        finally:
            self._owner = None

    async def close(self):
        if self._closed: return
        self._closed = True
        if self._watcher is not None:
            await self._watcher
        for task in (self._loader, self._owner):
            if task is not None:
                task.cancel()
                with contextlib.suppress(asyncio.CancelledError, ServiceError): await task
        await self._reap()


def _consume_exception(task):
    if not task.cancelled(): task.exception()
