export type CallEnvelope={input:unknown;expected_version?:number;idempotency_key?:string};
export type Capability={id:string;availability:string;input_schema:Record<string,unknown>};
export type Reply<T>={ok:boolean;data:T;meta:{operation_id?:string};error?:{code:string}};
export class APIError extends Error{
 constructor(public code:string,public status=0,public operationId?:string){super(code)}
}
export function secureOrigin(url:URL){return url.protocol==='https:'||(url.protocol==='http:'&&['localhost','127.0.0.1','[::1]'].includes(url.hostname))}
export function createManagementClient(token:string,fetchImpl:typeof fetch=fetch){
 const controller=new AbortController();
 async function request<T>(path:string,body?:CallEnvelope,signal?:AbortSignal):Promise<Reply<T>>{
  let response:Response;
  try{response=await fetchImpl(path,{method:body?'POST':'GET',headers:{Authorization:`Bearer ${token}`,...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined,credentials:'omit',redirect:'error',cache:'no-store',signal:signal?AbortSignal.any([signal,controller.signal]):controller.signal});}
  catch{throw new APIError('network_error')}
  let value:Reply<T>;
  try{value=await response.json()}catch{throw new APIError('invalid_response',response.status)}
  if(!response.ok||!value.ok)throw new APIError(value.error?.code||'request_failed',response.status,value.meta?.operation_id);
  return value;
 }
 return {schema:(signal?:AbortSignal)=>request<Capability[]>('/admin/v1/schema',undefined,signal),call:<T>(capability:string,envelope:CallEnvelope,signal?:AbortSignal)=>request<T>('/admin/v1/call/'+encodeURIComponent(capability),envelope,signal),dispose:()=>{token='';controller.abort()}};
}
export type Client=ReturnType<typeof createManagementClient>;
