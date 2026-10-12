import { createServer } from "node:http";
export async function startPlaygroundUpstream() {
  let probes = 0;
  const server = createServer(async (req, res) => {
    if (req.url === "/probe-count") {
      res.end(String(probes));
      return;
    }
    if (req.url === "/csp-probe") {
      probes++;
      res.end("probe");
      return;
    }
    let body = "";
    for await (const part of req) body += part;
    const input = JSON.parse(body);
    const prompt = input.messages.at(-1).content;
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    const event = (delta, finish_reason = null) =>
      res.write(
        `data: ${JSON.stringify({ id: "pg-mock", choices: [{ index: 0, delta, finish_reason }] })}\n\n`,
      );
    event({
      role: "assistant",
      reasoning_content: "hidden-reasoning-sentinel",
    });
    const content =
      prompt === "markdown"
        ? "**中文**\n\n```js\nconst answer = 42;\n```\n\n![external](https://pg-external.invalid/image)\n\n<script>window.PG_UNSAFE=true</script>\n\n[bad](javascript:alert(1))"
        : "Hello from the selected model.";
    const timer = setTimeout(
      () => {
        event({ content });
        event({ reasoning_content: "late-hidden-reasoning" }, "stop");
        res.end("data: [DONE]\n\n");
      },
      prompt === "slow" ? 30000 : 700,
    );
    res.on("close", () => clearTimeout(timer));
  });
  await new Promise((resolve) => server.listen(18764, "127.0.0.1", resolve));
  return server;
}
