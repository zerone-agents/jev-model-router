import {createRequire} from 'node:module';
import {readFileSync} from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const [baseURL,root]=process.argv.slice(2);
const require=createRequire(path.join(root,'package.json'));
const {default:Anthropic}=require('@anthropic-ai/sdk');
console.log('Anthropic SDK '+JSON.parse(readFileSync(path.join(root,'node_modules/@anthropic-ai/sdk/package.json'),'utf8')).version);
const client=new Anthropic({baseURL,apiKey:'router-key',maxRetries:0});
const request={model:'auto',max_tokens:2048,thinking:{type:'enabled',budget_tokens:1024},system:[{type:'text',text:'rules',cache_control:{type:'ephemeral',ttl:'1h'}}],tools:[{name:'lookup',input_schema:{type:'object',properties:{q:{type:'string'}},required:['q']},cache_control:{type:'ephemeral',ttl:'1h'}}],messages:[{role:'user',content:[{type:'text',text:'lookup',cache_control:{type:'ephemeral'}},{type:'image',source:{type:'base64',media_type:'image/png',data:'aGVsbG8='}},{type:'image',source:{type:'url',url:'https://image.invalid/never-download'}}]}]};
for(const model of ['auto','external'])for(const streaming of [false,true]){
 const call=async input=>streaming?client.messages.stream(input).finalMessage():client.messages.create(input);
 const first=await call({...request,model});assert.equal(first.model,'external');assert.equal(first.stop_reason,'tool_use');assert.equal(first.usage.input_tokens,0);assert.equal(first.usage.cache_read_input_tokens,20);assert.equal(first.content[0].signature,'signature-original');assert.equal(first.content[1].data,'opaque-original');assert.deepEqual(first.content[2].input,{q:'value'});
 const second=await call({...request,model,messages:[...request.messages,{role:'assistant',content:first.content},{role:'user',content:[{type:'tool_result',tool_use_id:'c',content:'failed',is_error:true}]}]});assert.equal(second.stop_reason,'end_turn');assert.equal(second.content[0].text,'answer');console.log(`${model}/${streaming?'SSE':'JSON'} signed thinking/redacted/tools/images/cache roundtrip passed`);
}
const truncated={...request,messages:[{role:'user',content:'truncate'}]};
const partial=await client.messages.create(truncated);assert.equal(partial.stop_reason,'max_tokens');assert.deepEqual(partial.content[0].input,{partial:true});
const events=[];for await(const event of await client.messages.create({...truncated,stream:true}))events.push(event);
assert.equal(events.find(e=>e.type==='content_block_delta').delta.partial_json,'{"q":');assert.equal(events.find(e=>e.type==='message_delta').delta.stop_reason,'max_tokens');assert.equal(events.at(-1).type,'message_stop');console.log('max_tokens JSON and low-level SSE preserved incomplete tool parameters');
try{const message=await client.messages.stream(truncated).finalMessage();assert.equal(message.stop_reason,'max_tokens');console.log('high-level truncated finalMessage returned: '+JSON.stringify(message.content));}catch(error){console.log('high-level truncated finalMessage exception: '+error.name+': '+error.message);}
const missing={model:'external',max_tokens:2048,messages:[{role:'user',content:'missing_usage'}]};
const noUsage=await client.messages.create(missing);assert.equal(Object.hasOwn(noUsage,'usage'),false);console.log('missing usage JSON remains absent');
const missingEvents=[];for await(const event of await client.messages.create({...missing,stream:true}))missingEvents.push(event);assert.equal(Object.hasOwn(missingEvents[0].message,'usage'),false);assert.equal(missingEvents.at(-1).type,'message_stop');
try{const message=await client.messages.stream(missing).finalMessage();console.log('high-level missing usage finalMessage: '+JSON.stringify(message.usage));}catch(error){console.log('high-level missing usage exception: '+error.name+': '+error.message);}
