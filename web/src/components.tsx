import {useEffect,useState} from 'react';
import {APIError,type Client} from './api';
import {text,type Lang} from './i18n';
export type PageProps={client:Client;lang:Lang;onError:(e:unknown)=>void};
export function errorText(e:unknown,lang:Lang){
 const code=e instanceof APIError?e.code:'';
 const map:Record<string,[string,string]>={unauthorized:['Credential expired or invalid. Reconnect to continue.','凭证无效或已过期，请重新连接。'],forbidden:['This credential cannot manage this instance.','此凭证没有管理权限。'],config_conflict:['Configuration changed. Read the latest version before saving again.','配置已变化，请读取最新版本后再保存。'],invalid_request:['Input does not match the instance contract.','输入不符合实例契约。'],network_error:['No response received. Check the connection.','未收到响应，请检查连接。'],invalid_response:['The instance returned an unreadable response.','实例返回了无法解析的响应。']};
 const pair=map[code]||['Request failed. Please check the instance and try again.','请求失败，请检查实例后重试。'];return text(lang,...pair);
}
export function ErrorBox({error,lang}:{error:unknown;lang:Lang}){return <div className="notice error" role="alert">{errorText(error,lang)}{error instanceof APIError&&error.operationId&&<small>{text(lang,'Operation','操作')} · {error.operationId}</small>}</div>}
export function useRead<T>({client,onError}:PageProps,id:string,input:unknown={},refresh=0){
 const [state,setState]=useState<{data?:T;error?:unknown;loading:boolean}>({loading:true});const key=JSON.stringify(input);
 useEffect(()=>{let active=true;const abort=new AbortController();setState({loading:true});client.call<T>(id,{input:JSON.parse(key)},abort.signal).then(r=>{if(active)setState({data:r.data,loading:false})}).catch(e=>{if(active){setState({error:e,loading:false});onError(e)}});return()=>{active=false;abort.abort()}},[client,id,key,refresh,onError]);
 return state;
}
export function Loading(){return <div className="skeleton" role="status" aria-label="Loading"><span/><span/><span/></div>}
export function PageTitle({title,description,children}:{title:string;description:string;children?:React.ReactNode}){return <header className="page-title"><div><h1>{title}</h1><p>{description}</p></div>{children}</header>}
export function Empty({title,detail}:{title:string;detail:string}){return <div className="empty"><span className="empty-line"/><h3>{title}</h3><p>{detail}</p></div>}
export function Pager({next,back,onNext,onBack,lang}:{next:string;back:boolean;onNext:()=>void;onBack:()=>void;lang:Lang}){return <div className="pager"><button disabled={!back} onClick={onBack}>{text(lang,'Previous','上一页')}</button><button disabled={!next} onClick={onNext}>{text(lang,'Next','下一页')}</button></div>}
