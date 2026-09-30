// Invoked by TestUnmodifiedAgentSDK against a local router and mock upstream.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
const [sdk, baseURL] = process.argv.slice(2);
const pkg = JSON.parse(await readFile(resolve(sdk, 'package.json'), 'utf8'));
assert.equal(pkg.version, '4.0.0');
const { OpenAIProvider } = await import(pathToFileURL(resolve(sdk, 'src/providers/openai.ts')));
const provider = new OpenAIProvider({ apiKey: 'local-test', baseURL });
for (const model of ['auto', 'external']) {
  const params = { model, maxTokens: 1024, system: 'Reply briefly.', messages: [{ role: 'user', content: '请用一句中文解释模型路由。' }] };
  const result = await provider.createMessage(params);
  assert.equal(result.stopReason, 'end_turn');
  assert.ok(JSON.stringify(result).includes('ok'));
  const chunks = [];
  for await (const chunk of provider.createMessageStream(params)) chunks.push(chunk);
  assert.ok(JSON.stringify(chunks).includes('ok'));
  console.log(`${model}: createMessage and createMessageStream passed`);
}
