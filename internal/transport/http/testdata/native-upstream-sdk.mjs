import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
import path from 'node:path';
import assert from 'node:assert/strict';
const [baseURL,root,agent,mode]=process.argv.slice(2);
if(mode==='openai'){
 const require=createRequire(path.join(root,'package.json'));
 const {default:OpenAI}=require('openai');
 const client=new OpenAI({baseURL,apiKey:'local',maxRetries:0});
 for(const model of ['auto','external']) for(const stream of [false,true]){
  const tools=[{type:'function',function:{name:'lookup',description:'lookup',parameters:{type:'object',properties:{q:{type:'string'}},required:['q']}}}];
  const messages=[{role:'user',content:'lookup'}];
  const request=async()=>{
   const result=await client.chat.completions.create({model,messages,tools,reasoning_effort:'none',stream,...(stream?{stream_options:{include_usage:true}}:{})});
   if(!stream){assert.equal(result.usage.prompt_tokens,60);return result.choices[0].message;}
   const message={role:'assistant',content:'',tool_calls:[]};let terminal=false,usage;
   for await(const c of result){if(c.usage)usage=c.usage;for(const choice of c.choices){if(choice.finish_reason)terminal=true;message.content+=choice.delta.content??'';for(const t of choice.delta.tool_calls??[]){const v=message.tool_calls[t.index]??={id:'',type:'function',function:{name:'',arguments:''}};if(t.id)v.id=t.id;if(t.function?.name)v.function.name=t.function.name;v.function.arguments+=t.function?.arguments??'';}}}
   assert(terminal);assert.equal(usage.prompt_tokens,60);return message;
  };
  const first=await request();assert.equal(first.tool_calls.length,1);assert.deepEqual(JSON.parse(first.tool_calls[0].function.arguments),{q:'value'});
  messages.push(first,{role:'tool',tool_call_id:first.tool_calls[0].id,content:'tool-result-value'});
  const second=await request();assert.equal(second.content,'answer');
  console.log(`OpenAI SDK ${model} stream=${stream} tool roundtrip passed`);
 }
}else{
 const {OpenAIProvider}=await import(pathToFileURL(path.join(agent,'src/providers/openai.ts')));
 const provider=new OpenAIProvider({baseURL,apiKey:'local'});
 const {StreamAccumulator}=await import(pathToFileURL(path.join(agent,'src/engine/stream-parser.ts')));
 for(const streaming of [false,true]){
 const params={model:'auto',maxTokens:1024,effort:'none',system:'reply briefly',messages:[{role:'user',content:'lookup'}],tools:[{name:'lookup',description:'lookup',input_schema:{type:'object',properties:{q:{type:'string'}},required:['q']}}]};
 const call=async()=>{
  if(!streaming)return provider.createMessage(params);
  const accumulator=new StreamAccumulator();
  for await(const c of provider.createMessageStream(params)) accumulator.addChunk(c);
  return accumulator.buildResponse();
 };
 const first=await call();const tool=first.content.find(c=>c.type==='tool_use');assert(tool);assert.deepEqual(tool.input,{q:'value'});
 params.messages.push({role:'assistant',content:first.content},{role:'user',content:[{type:'tool_result',tool_use_id:tool.id,content:'tool-result-value'}]});
 const second=await call();assert(second.content.some(c=>c.type==='text'&&c.text==='answer'));
 console.log(`Agent SDK stream=${streaming} tool roundtrip passed`);
 }
}
