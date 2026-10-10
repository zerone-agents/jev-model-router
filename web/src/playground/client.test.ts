import { expect, it } from 'vitest';
import { consumePlayground } from './client';
it('decodes UTF-8 split across chunks and requires a terminal event', async()=>{
 const bytes=new TextEncoder().encode('event: delta\r\ndata: {"content":"中文"}\r\n\r\nevent: done\ndata: {"finish_reason":"stop"}\n\n');
 const response=new Response(new ReadableStream({start(c){for(const b of bytes)c.enqueue(new Uint8Array([b]));c.close()}}),{headers:{'Content-Type':'text/event-stream'}});
 const events: unknown[]=[];await consumePlayground(response,e=>events.push(e));expect(events).toEqual([{type:'delta',content:'中文'},{type:'done',finish_reason:'stop'}]);
 await expect(consumePlayground(new Response('event: delta\ndata: {"content":"partial"}\n\n',{headers:{'Content-Type':'text/event-stream'}}),()=>{})).rejects.toThrow('interrupted');
});
it('rejects invalid events and preserves rate-limit metadata',async()=>{
 await expect(consumePlayground(new Response('event: delta\ndata: {"content":3}\n\n',{headers:{'Content-Type':'text/event-stream'}}),()=>{})).rejects.toThrow('invalid_response');
 await expect(consumePlayground(new Response(JSON.stringify({error:{code:'playground_rate_limited',retry_after_seconds:60,limit_scope:'day'}}),{status:429,headers:{'Retry-After':'60'}}),()=>{})).rejects.toMatchObject({code:'playground_rate_limited',retryAfter:60,scope:'day'});
});
