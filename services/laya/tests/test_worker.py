import asyncio
import os
import time
import unittest

from jev_laya.worker import Supervisor
from jev_laya.protocol import ServiceError


class ControlledEvaluator:
    def __call__(self, payload):
        if payload == b'crash': os._exit(2)
        time.sleep(float(payload))
        return {'pid': os.getpid()}


def controlled_factory(settings):
    return ControlledEvaluator()


class WorkerTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.supervisor = Supervisor(None, timeout=0.5, factory=controlled_factory)
        self.supervisor.start()
        await self.wait_ready()

    async def asyncTearDown(self):
        await self.supervisor.close()

    async def wait_ready(self):
        async with asyncio.timeout(5):
            while not self.supervisor.ready(): await asyncio.sleep(0.01)

    async def test_zero_queue_and_cancel_does_not_release_slot(self):
        task = asyncio.create_task(self.supervisor.submit(b'0.2'))
        await asyncio.sleep(0.03)
        with self.assertRaises(ServiceError) as caught: await self.supervisor.submit(b'0')
        self.assertEqual(caught.exception.code, 'overloaded')
        task.cancel()
        with self.assertRaises(asyncio.CancelledError): await task
        with self.assertRaises(ServiceError) as caught: await self.supervisor.submit(b'0')
        self.assertEqual(caught.exception.code, 'overloaded')
        await asyncio.sleep(0.25)
        self.assertIn('pid', await self.supervisor.submit(b'0'))

    async def test_deadline_reaps_worker_before_replacement(self):
        first = await self.supervisor.submit(b'0')
        with self.assertRaises(ServiceError) as caught: await self.supervisor.submit(b'5')
        self.assertEqual(caught.exception.code, 'model_unavailable')
        self.assertFalse(self.supervisor.ready())
        await self.wait_ready()
        second = await self.supervisor.submit(b'0')
        self.assertNotEqual(first['pid'], second['pid'])
        with self.assertRaises(ProcessLookupError): os.kill(first['pid'], 0)

    async def test_crash_recovers_and_shutdown_is_bounded(self):
        with self.assertRaises(ServiceError): await self.supervisor.submit(b'crash')
        await self.wait_ready()
        running = asyncio.create_task(self.supervisor.submit(b'5'))
        await asyncio.sleep(0.03)
        await asyncio.wait_for(self.supervisor.close(), 2)
        with self.assertRaises((ServiceError, asyncio.CancelledError)): await running
        self.assertFalse(self.supervisor.ready())
