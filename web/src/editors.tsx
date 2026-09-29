import {useEffect,useRef,useState} from 'react';
import {ErrorBox,Loading,PageTitle,useRead,type PageProps} from './components';
import {text} from './i18n';
import {WriteAttempt,prepareWrite,descriptionInput} from './edit';
import type {Model,Resource} from './pages';
type Snapshot={version:number;text:string;model?:Model};
type Props=PageProps&{canEdit:boolean;onDirty:(value:boolean)=>void};
export function PromptEditor(props:Props){const state=useRead<{text:string;version:number}>(props,'prompt.get');return <><PageTitle title={text(props.lang,'Routing prompt','路由提示词')} description={text(props.lang,'One balanced template. Your routing preferences.','一份均衡模板，表达你的选模偏好。')}/>{state.loading?<Loading/>:state.error?<ErrorBox error={state.error} lang={props.lang}/>:state.data&&<Editor {...props} initial={state.data}/>}</>}
export function ModelEditor(props:Props&{id:string}){const state=useRead<Resource>(props,'models.get',{id:props.id});return state.loading?<Loading/>:state.error?<ErrorBox error={state.error} lang={props.lang}/>:state.data&&<Editor {...props} initial={{version:state.data.version,text:state.data.resource.description,model:state.data.resource}}/>}
function Editor(props:Props&{initial:Snapshot}){
 const{client,lang,onError,onDirty,canEdit}=props;const[snapshot,setSnapshot]=useState(props.initial);const[draft,setDraft]=useState(props.initial.text);const[attempt,setAttempt]=useState<WriteAttempt|null>(null);const[busy,setBusy]=useState(false);const[error,setError]=useState<unknown>();const[notice,setNotice]=useState<'saved'|'reloaded'|null>(null);const alive=useRef(true);const working=useRef(false);
 useEffect(()=>{alive.current=true;return()=>{alive.current=false}},[]);
 const unresolved=attempt?.state==='unknown';const conflict=attempt?.state==='conflict';const dirty=draft!==snapshot.text||unresolved||false;
 useEffect(()=>{onDirty(dirty);return()=>onDirty(false)},[dirty,onDirty]);
 async function save(retry=false){
  if(working.current)return;working.current=true;setBusy(true);setError(undefined);setNotice(null);
  const job=retry?attempt!:new WriteAttempt(prepareWrite(snapshot.model?'models.put':'prompt.put',snapshot.model?descriptionInput(snapshot.model,draft):{text:draft},snapshot.version,crypto.randomUUID()));setAttempt(job);
  try{const result=await job.send<{version:number}>(client);if(!alive.current)return;const submitted=JSON.parse(job.write.serialized).input;setSnapshot({...snapshot,version:result.data.version,text:snapshot.model?submitted.description:submitted.text,model:snapshot.model?submitted:undefined});setNotice('saved')}
  catch(e){if(alive.current){setError(e);onError(e)}}finally{working.current=false;if(alive.current)setBusy(false)}
 }
 async function reload(){
  if(working.current)return;working.current=true;setBusy(true);setError(undefined);
  try{const result=snapshot.model?await client.call<Resource>('models.get',{input:{id:snapshot.model.id}}):await client.call<{version:number;text:string}>('prompt.get',{input:{}});if(!alive.current)return;
   const data=result.data;const next:Snapshot='resource'in data?{version:data.version,text:data.resource.description,model:data.resource}:data;
   setSnapshot(next);setAttempt(null);setNotice('reloaded');
  }catch(e){if(alive.current){setError(e);onError(e)}}finally{working.current=false;if(alive.current)setBusy(false)}
 }
 return <section className="editor-card">{snapshot.model&&<><h2>{snapshot.model.id}</h2><div className="editor-meta"><small>{snapshot.model.provider_id} / {snapshot.model.upstream_name}</small><span className="badge">{text(lang,snapshot.model.enabled?'Enabled':'Disabled',snapshot.model.enabled?'已启用':'已禁用')}</span></div><div className="capabilities">{Object.entries(snapshot.model.capabilities).map(([k,v])=><span className="badge" key={k}>{k}: {String(v)}</span>)}</div><p className="read-only">{text(lang,'Model mapping and capabilities are managed through the CLI.','模型映射和能力通过 CLI 管理。')}</p></>}
 <label htmlFor="editor-text">{text(lang,snapshot.model?'Model description':'Prompt content',snapshot.model?'模型描述':'提示词内容')}</label><textarea id="editor-text" value={draft} onChange={e=>{setDraft(e.target.value);setNotice(null)}} readOnly={!canEdit} disabled={busy||unresolved} rows={snapshot.model?7:15}/>
 {!canEdit&&<p className="read-only">{text(lang,'Editing is not available on this instance.','此实例未提供编辑能力。')}</p>}
 {error!=null&&<ErrorBox error={error} lang={lang}/>} {unresolved&&<div className="notice">{text(lang,'Save outcome is unknown. Retry the identical request within 24 hours, or read the latest configuration to reconcile. No automatic retry was made.','保存结果未知。可在 24 小时内重试相同请求，或读取最新配置核对。系统没有自动重试。')}</div>}
 {notice==='saved'&&<p role="status" className="success">{text(lang,'Saved','已保存')} · v{snapshot.version}</p>}
 {notice==='reloaded'&&<div role="status" className="notice">{text(lang,'Latest version loaded. Your draft is retained. Compare it with the saved value below before saving again.','已读取最新版本，草稿仍保留。再次保存前，请与下方已保存内容比较。')}<pre>{snapshot.text}</pre></div>}
 <div className="editor-actions"><small>v{snapshot.version} · {text(lang,dirty?'Unsaved changes':'Up to date',dirty?'有未保存的修改':'已是最新内容')}</small><div>{(conflict||unresolved)&&<button disabled={busy} onClick={reload}>{text(lang,'Read latest version','读取最新版本')}</button>}{unresolved?<button className="primary" disabled={busy} onClick={()=>save(true)}>{text(lang,'Retry same request','重试相同请求')}</button>:canEdit&&<button className="primary" disabled={busy||!dirty||conflict} onClick={()=>save()}>{text(lang,busy?'Saving…':'Save changes',busy?'正在保存…':'保存修改')}</button>}</div></div></section>;
}
