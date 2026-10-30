import asyncio
import unittest

import httpx
from jev_laya.app import create_app
from jev_laya.config import Settings
from jev_laya.protocol import ServiceError


class AdmissionStub:
    def __init__(self): self.calls = 0; self.available = True
    def start(self): pass
    async def close(self): pass
    def ready(self): return self.available
    async def submit(self, body):
        self.calls += 1
        if body == b'fail': raise RuntimeError('SECRET conversation')
        if body == b'busy': raise ServiceError('overloaded', 'Model worker is busy; retry later.', 429)
        return {'answers': {'model': {'type':'choice','choice':'a'}}}


class HTTPTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.worker = AdmissionStub()
        self.app = create_app(Settings(token='private-token', model_dir='/unused'), self.worker)
        self.client = httpx.AsyncClient(transport=httpx.ASGITransport(app=self.app), base_url='http://test')

    async def asyncTearDown(self): await self.client.aclose()

    async def test_auth_and_health_boundary(self):
        self.assertEqual((await self.client.post('/v1/systemone', content=b'{}')).status_code, 401)
        self.assertEqual(self.worker.calls, 0)
        self.assertEqual((await self.client.get('/metadata')).status_code, 401)
        self.assertEqual((await self.client.get('/health/live')).status_code, 200)
        self.worker.available = False
        self.assertEqual((await self.client.get('/health/ready')).status_code, 503)

    async def test_body_limit_streaming_and_no_secret_in_errors(self):
        headers = {'Authorization': 'Bearer private-token', 'Content-Type': 'application/json'}
        async def chunks():
            yield b' ' * 40000
            yield b' ' * 40000
        response = await self.client.post('/v1/systemone', content=chunks(), headers=headers)
        self.assertEqual(response.status_code, 413)
        self.assertEqual(self.worker.calls, 0)
        response = await self.client.post('/v1/systemone', content=b'fail', headers=headers)
        self.assertEqual(response.status_code, 500)
        self.assertNotIn('SECRET', response.text)
        self.assertNotIn('private-token', response.text)
        response = await self.client.post('/v1/systemone', content=b'busy', headers=headers)
        self.assertEqual(response.status_code, 429)
        response = await self.client.post('/v1/systemone', content=b'{}', headers=headers)
        self.assertEqual(response.json()['answers']['model']['choice'], 'a')

    async def test_metadata_requires_auth_and_exposes_budget(self):
        response = await self.client.get('/metadata', headers={'Authorization': 'Bearer private-token'})
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()['head_tokens'], 512)
        self.assertNotIn('private-token', response.text)
