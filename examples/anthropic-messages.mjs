// Install @anthropic-ai/sdk in the project running this example.
import Anthropic from '@anthropic-ai/sdk';

const client = new Anthropic({
  baseURL: process.env.JEV_ROUTER_BASE_URL ?? 'http://127.0.0.1:8080',
  apiKey: process.env.JEV_ROUTER_INFERENCE_KEY,
  maxRetries: 0,
});
const request = {
  model: process.env.JEV_ROUTER_MODEL ?? 'auto',
  max_tokens: 1024,
  thinking: { type: 'adaptive' },
  output_config: { effort: 'high' },
  messages: [{ role: 'user', content: 'Explain why the sky is blue.' }],
};
const stream = client.messages.stream(request);
stream.on('text', (text) => process.stdout.write(text));
const message = await stream.finalMessage();
console.log('\n', message.stop_reason, message.usage);
