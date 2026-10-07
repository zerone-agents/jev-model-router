import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';

const [baseURL, root] = process.argv.slice(2);
const require = createRequire(path.join(root, 'package.json'));
const { default: Anthropic } = require('@anthropic-ai/sdk');
const pkg = JSON.parse(readFileSync(path.join(root, 'node_modules/@anthropic-ai/sdk/package.json'), 'utf8'));
console.log(`Anthropic SDK ${pkg.version}`);
const client = new Anthropic({ baseURL, apiKey: 'router-key', maxRetries: 0 });
for (const model of ['auto', 'external']) {
  for (const streaming of [false, true]) {
    const request = {
      model, max_tokens: 1024,
      thinking: { type: 'adaptive' }, output_config: { effort: 'high' },
      messages: [{ role: 'user', content: [
        { type: 'text', text: 'lookup' },
        { type: 'image', source: { type: 'base64', media_type: 'image/png', data: 'aGVsbG8=' } },
      ] }],
      tools: [{ name: 'lookup', description: 'Lookup a value', input_schema: { type: 'object', properties: { q: { type: 'string' } }, required: ['q'] } }],
      tool_choice: { type: 'auto', disable_parallel_tool_use: true },
    };
    const call = async (input) => {
      if (!streaming) return client.messages.create(input);
      const stream = client.messages.stream(input);
      const message = await stream.finalMessage();
      assert.equal(message.usage.input_tokens, 7);
      assert.equal(message.usage.output_tokens, 3);
      return message;
    };
    const first = await call(request);
    assert.equal(first.model, 'external');
    assert.equal(first.stop_reason, 'tool_use');
    const thought = first.content.find((b) => b.type === 'thinking');
    assert.equal(thought.thinking, 'thought');
    const tool = first.content.find((b) => b.type === 'tool_use');
    assert.equal(tool.id, 'call1');
    assert.deepEqual(tool.input, { q: 'value' });
    const second = await call({ ...request, messages: [
      ...request.messages, { role: 'assistant', content: first.content },
      { role: 'user', content: [{ type: 'tool_result', tool_use_id: tool.id, content: 'result' }] },
    ] });
    assert.equal(second.stop_reason, 'end_turn');
    assert.equal(second.content.find((b) => b.type === 'text').text, 'answer');
    console.log(`${model}/${streaming ? 'SSE' : 'JSON'} tool+thinking round trip passed`);
  }
}
await assert.rejects(client.messages.create({ model: 'auto', max_tokens: 12, messages: [{ role: 'user', content: 'invalid' }] }), (error) => error.status === 400 && error.error.type === 'error');
const invalidClient = new Anthropic({ baseURL, apiKey: 'bad-key', maxRetries: 0 });
await assert.rejects(invalidClient.messages.create({ model: 'auto', max_tokens: 1024, messages: [{ role: 'user', content: 'query' }] }), (error) => error.status === 401 && error.error.error.type === 'authentication_error' && typeof error.error.request_id === 'string');
await assert.rejects(client.messages.stream({ model: 'auto', max_tokens: 1024, messages: [{ role: 'user', content: 'force_stream_failure' }] }).finalMessage(), (error) => error instanceof Error && /provider failed/.test(error.message));
console.log('SDK authentication and midstream error parsing passed');
