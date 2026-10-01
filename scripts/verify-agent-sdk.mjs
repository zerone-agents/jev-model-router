// Invoked by TestUnmodifiedAgentSDK against a local router and mock upstream.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
const [sdk, baseURL, suite] = process.argv.slice(2);
const pkg = JSON.parse(await readFile(resolve(sdk, "package.json"), "utf8"));
console.log(`agent-sdk ${pkg.version}`);
const { OpenAIProvider } = await import(
  pathToFileURL(resolve(sdk, "src/providers/openai.ts"))
);
const provider = new OpenAIProvider({ apiKey: "local-test", baseURL });
for (const model of ["auto", "external"]) {
  const settings =
    suite === "reasoning"
      ? [
          { thinking: { type: "enabled" } },
          { effort: "high" },
          { thinking: { type: "enabled" }, effort: "high" },
        ]
      : [{}];
  for (const controls of settings) {
    const params = {
      ...controls,
      model,
      maxTokens: 1024,
      system: "Reply briefly.",
      messages: [{ role: "user", content: "请用一句中文解释模型路由。" }],
    };
    const result = await provider.createMessage(params);
    assert.equal(result.stopReason, "end_turn");
    assert.ok(JSON.stringify(result).includes("ok"));
    const chunks = [];
    for await (const chunk of provider.createMessageStream(params))
      chunks.push(chunk);
    assert.ok(JSON.stringify(chunks).includes("ok"));
    assert.equal(chunks.at(-1)?.type, "done");
    if (suite === "reasoning") {
      assert.ok(
        result.content.some(
          (block) => block.type === "thinking" && block.thinking === "thought",
        ),
      );
      assert.ok(
        chunks.some(
          (chunk) => chunk.type === "thinking" && chunk.delta === "thought",
        ),
      );
    }
    console.log(
      `${model} ${JSON.stringify(controls)}: createMessage and createMessageStream passed`,
    );
  }
}
