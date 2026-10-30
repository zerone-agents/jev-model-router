import asyncio
import socket
import unittest

import httpx
import uvicorn
from jev_laya.app import create_app
from jev_laya.config import Settings
from jev_laya.worker import Supervisor
from test_worker import controlled_factory


class SocketTests(unittest.IsolatedAsyncioTestCase):
    async def test_disconnect_does_not_release_computing_worker(self):
        supervisor = Supervisor(None, timeout=2, factory=controlled_factory)
        app = create_app(Settings(token='test-only',model_dir='/unused'), supervisor)
        listener = socket.socket()
        listener.bind(('127.0.0.1',0))
        listener.listen()
        port = listener.getsockname()[1]
        server = uvicorn.Server(uvicorn.Config(app, log_level='critical', access_log=False, lifespan='on'))
        serving = asyncio.create_task(server.serve(sockets=[listener]))
        try:
            async with asyncio.timeout(5):
                while not supervisor.ready(): await asyncio.sleep(0.01)
            reader, writer = await asyncio.open_connection('127.0.0.1',port)
            writer.write(b'POST /v1/systemone HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer test-only\r\nContent-Type: application/json\r\nContent-Length: 3\r\n\r\n0.5')
            await writer.drain()
            await asyncio.sleep(0.05)
            writer.close(); await writer.wait_closed()
            async with httpx.AsyncClient(base_url=f'http://127.0.0.1:{port}') as client:
                response = await client.post('/v1/systemone', content=b'0', headers={'Authorization':'Bearer test-only','Content-Type':'application/json'})
                self.assertEqual(response.status_code,429)
                await asyncio.sleep(0.55)
                response = await client.post('/v1/systemone', content=b'0', headers={'Authorization':'Bearer test-only','Content-Type':'application/json'})
                self.assertEqual(response.status_code,200)
        finally:
            server.should_exit = True
            await asyncio.wait_for(serving,5)
            listener.close()
