import { APIError, type Client } from '../api';
export type Message = {role:'user'|'assistant';content:string;reasoning_content?:string};
export type PlaygroundInput={model:string;messages:Message[];stream:true};
export type Route={request_id:string;model_id:string;path:string;config_version:number;decision_ms:number};
export type PlaygroundEvent=({type:'route'} & Route)|{type:'delta';content?:string;reasoning_content?:string}|{type:'done';finish_reason:string;usage?:{input_tokens:number;output_tokens:number;total_tokens?:number}}|{type:'error';code:string;message:string};
export type PlaygroundStatus={enabled:boolean;limits:{input_bytes:number;max_messages:number;output_tokens:number;daily_requests:number;session_rpm:number;instance_rpm:number};timeout_seconds:number;quota:{day_remaining:number;session_remaining:number;instance_remaining:number;reset_at:string}};
export class PlaygroundError extends APIError{constructor(code:string,status=0,public retryAfter=0,public scope=''){super(code,status)}}
function object(v:unknown):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)}
function decodeEvent(type:string,data:string):PlaygroundEvent{
 let v:unknown;try{v=JSON.parse(data)}catch{throw new APIError('invalid_response')}
 if(!object(v))throw new APIError('invalid_response');
 const strings=(keys:string[])=>keys.every(k=>typeof v[k]==='string');
 const optional=(keys:string[])=>keys.every(k=>v[k]===undefined||typeof v[k]==='string');
 const numbers=(keys:string[])=>keys.every(k=>typeof v[k]==='number'&&Number.isFinite(v[k]));
 if(type==='route'&&strings(['request_id','model_id','path'])&&numbers(['config_version','decision_ms']))return {...v,type} as PlaygroundEvent;
 if(type==='delta'&&optional(['content','reasoning_content']))return {...v,type} as PlaygroundEvent;
 if(type==='done'&&strings(['finish_reason'])&&['stop','length','content_filter'].includes(v.finish_reason as string))return {...v,type} as PlaygroundEvent;
 if(type==='error'&&strings(['code','message']))return {...v,type} as PlaygroundEvent;
 throw new APIError('invalid_response');
}
export async function consumePlayground(response:Response,onEvent:(e:PlaygroundEvent)=>void):Promise<void>{
 if(!response.ok){let b:unknown;try{b=await response.json()}catch{throw new APIError('invalid_response',response.status)}
 if(!object(b)||!object(b.error)||typeof b.error.code!=='string')throw new APIError('invalid_response',response.status);
 const retry=Number(response.headers.get('Retry-After')||b.error.retry_after_seconds||0);
 throw new PlaygroundError(b.error.code,response.status,Number.isFinite(retry)?Math.max(0,retry):0,typeof b.error.limit_scope==='string'?b.error.limit_scope:'');}
 if(!response.headers.get('Content-Type')?.includes('text/event-stream')||!response.body)throw new APIError('invalid_response');
 const reader=response.body.getReader(),decoder=new TextDecoder('utf-8',{fatal:true});let buffer='',terminal=false,type='',data:string[]=[];
 const line=(s:string)=>{if(!s){if(data.length){const e=decodeEvent(type,data.join('\n'));onEvent(e);if(e.type==='error')throw new APIError(e.code);if(e.type==='done')terminal=true;}type='';data=[];}else if(s.startsWith('event:'))type=s.slice(6).trim();else if(s.startsWith('data:'))data.push(s.slice(5).replace(/^ /,''));};
 try{while(!terminal){const {done,value}=await reader.read();buffer+=decoder.decode(value,{stream:!done});if(buffer.length+data.join('\n').length>262144)throw new APIError('invalid_response');let i:number;while((i=buffer.indexOf('\n'))>=0&&!terminal){line(buffer.slice(0,i).replace(/\r$/,''));buffer=buffer.slice(i+1)}if(done)break;}if(!terminal)throw new APIError('interrupted');}finally{await reader.cancel().catch(()=>{});reader.releaseLock()}
}
export async function streamPlayground(client:Client,input:PlaygroundInput,signal:AbortSignal,onEvent:(e:PlaygroundEvent)=>void){await consumePlayground(await client.playground(input,signal),onEvent)}
export async function readPlaygroundStatus(client:Client,signal:AbortSignal):Promise<PlaygroundStatus>{const response=await client.playground(undefined,signal);if(!response.ok){await consumePlayground(response,()=>{});throw new APIError('invalid_response')};let v:unknown;try{v=await response.json()}catch{throw new APIError('invalid_response')};if(!object(v)||typeof v.enabled!=='boolean'||!object(v.limits)||!object(v.quota)||typeof v.quota.day_remaining!=='number'||typeof v.quota.reset_at!=='string'||!Number.isFinite(Date.parse(v.quota.reset_at))||!['input_bytes','max_messages','output_tokens','daily_requests','session_rpm','instance_rpm'].every(k=>typeof (v.limits as Record<string,unknown>)[k]==='number'))throw new APIError('invalid_response');return v as PlaygroundStatus}
