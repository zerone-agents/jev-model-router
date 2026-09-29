import {it,expect,vi} from 'vitest';
import {prepareWrite,WriteAttempt,descriptionInput} from './edit';
import {APIError,createManagementClient} from './api';
it('description replacement preserves every other field',()=>{const model={id:'m',description:'old',enabled:false,capabilities:{tools:true},future:'keep'};expect(descriptionInput(model,'new')).toEqual({...model,description:'new'});expect(model.description).toBe('old')});
it('captures immutable input and retries byte-identically',async()=>{
 const input={text:'a'};const body=prepareWrite('prompt.put',input,2,'key');input.text='b';
 const f=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(new Response(JSON.stringify({ok:true,data:{version:3},meta:{}})));
 const a=new WriteAttempt(body,100);const c=createManagementClient('x',f);
 await expect(a.send(c,101)).rejects.toBeInstanceOf(APIError);await a.send(c,102);
 expect(f.mock.calls[0][1].body).toBe(f.mock.calls[1][1].body);expect(JSON.parse(f.mock.calls[1][1].body).input.text).toBe('a');expect(a.state).toBe('saved');
});
it('prevents simultaneous sends and retry at expiry',async()=>{
 let resolve!:(r:Response)=>void;const c=createManagementClient('x',vi.fn(()=>new Promise<Response>(r=>resolve=r)));const a=new WriteAttempt(prepareWrite('prompt.put',{text:'a'},1,'k'),0);
 const first=a.send(c,1);await expect(a.send(c,2)).rejects.toMatchObject({code:'write_busy'});resolve(new Response(JSON.stringify({ok:true,data:{version:2},meta:{}})));await first;
 const expired=new WriteAttempt(prepareWrite('prompt.put',{},1,'k'),0);await expect(expired.send(c,86400000)).rejects.toMatchObject({code:'retry_expired'});
});
it('conflict remains distinct from unknown outcome',async()=>{const c=createManagementClient('x',vi.fn().mockResolvedValue(new Response(JSON.stringify({ok:false,error:{code:'config_conflict'},meta:{}}),{status:409})));const a=new WriteAttempt(prepareWrite('prompt.put',{},1,'k'));await expect(a.send(c)).rejects.toMatchObject({code:'config_conflict'});expect(a.state).toBe('conflict')});
