"""Authenticated, bounded HTTP entry; inference ownership stays in Supervisor."""
import asyncio
from contextlib import asynccontextmanager, suppress
import hmac

from starlette.applications import Starlette
from starlette.requests import ClientDisconnect
from starlette.responses import JSONResponse, Response
from starlette.routing import Route

from .protocol import BODY_LIMIT, ServiceError


def create_app(settings, supervisor):
    @asynccontextmanager
    async def lifespan(app):
        supervisor.start()
        try: yield
        finally: await supervisor.close()

    def authenticated(request):
        value = request.headers.get('authorization', '')
        return hmac.compare_digest(value.encode(), ('Bearer ' + settings.token).encode())

    def error_response(error):
        return JSONResponse({'error': {'code': error.code, 'message': error.message}}, status_code=error.status)

    async def live(request):
        return JSONResponse({'status': 'live'})

    async def ready(request):
        ok = supervisor.ready()
        return JSONResponse({'status': 'ready' if ok else 'unavailable'}, status_code=200 if ok else 503)

    async def metadata(request):
        if not authenticated(request):
            return error_response(ServiceError('unauthorized', 'Authentication required.', 401))
        return JSONResponse({'model': settings.alias, 'model_revision': '1720e3e3357cfe1e281542e223f8273b0890ca34',
                             'device': settings.device, 'threads': settings.threads, 'context_tokens': settings.context,
                             'head_tokens': settings.head, 'body_bytes': BODY_LIMIT, 'max_questions': 2,
                             'criteria_parser_limit': 64, 'question_types': ['choice'],
                             'usage': 'Actual unpadded model positions across evaluated questions; singleton work is zero.',
                             'worker_timeout_seconds': settings.deadline})

    async def disconnected(request):
        while True:
            message = await request.receive()
            if message['type'] == 'http.disconnect': return

    async def decision(request):
        if not authenticated(request):
            return error_response(ServiceError('unauthorized', 'Authentication required.', 401))
        work = monitor = None
        try:
            if request.headers.get('content-type', '').split(';')[0].strip().lower() != 'application/json':
                raise ServiceError('invalid_request', 'Content-Type must be application/json.')
            body = bytearray()
            async for part in request.stream():
                if len(body) + len(part) > BODY_LIMIT:
                    raise ServiceError('input_too_large', 'HTTP body exceeds 65536 bytes.', 413)
                body.extend(part)
            work = asyncio.create_task(supervisor.submit(bytes(body)))
            monitor = asyncio.create_task(disconnected(request))
            done, _ = await asyncio.wait((work, monitor), return_when=asyncio.FIRST_COMPLETED)
            if monitor in done:
                work.cancel()
                return Response(status_code=499)
            return JSONResponse(await work)
        except ClientDisconnect:
            return Response(status_code=499)
        except ServiceError as error:
            return error_response(error)
        except Exception:
            return error_response(ServiceError('inference_error', 'Model request failed.', 500))
        finally:
            for task in (work, monitor):
                if task is not None:
                    task.cancel()
                    with suppress(asyncio.CancelledError, Exception): await task

    return Starlette(lifespan=lifespan, routes=[Route('/health/live', live), Route('/health/ready', ready),
                                              Route('/metadata', metadata), Route('/v1/systemone', decision, methods=['POST'])])
