# SDK examples 经 router 验收

SDK 本地版本 3.5.1；直接执行 SDK 仓库原始 examples，通过环境变量设置 OpenAI-compatible API、router 地址与 `auto` 模型。未改动 SDK 源码或缩短工具描述。工作目录是临时 fixture（package.json 与最小 src/agent.ts），不使用 SDK 仓库作为工具执行目录。

生成上游：阿里云兼容 API，qwen3.8-flash。使用一个合格模型，auto 走唯一候选路径；本轮未验证真实 Jev 多候选选择。凭证和原始请求不写入本记录。

## 复现与修复

原始 basic/01-simple-query.ts 加载 13 个默认工具：Bash 描述 4407 字符，Task 描述 4109 字符。旧 schema 限制 4096，导致 sample 返回 error 且没有生成输出。移除 tools.function.description 的局部长度限制，并同步 route.inspect；不截断工具说明，其余字段约束与总请求/上下文预算继续生效。

此前只运行 Provider 层测试，没有覆盖 createAgent 默认工具池，因而漏掉此问题。新增无需真实 Key 的原始 sample 回归：模拟模型发出 Read 调用，SDK 实际读取 fixture，第二轮验证工具结果并完成回复。逐请求比较 SDK 与上游的工具定义，JSON/SSE × auto/显式模型四种组合均通过。

## 真实上游结果

下表请求数包含 sample 内部工具往返与会话压缩，不表示用户 query 数。所有列为通过的请求均无 HTTP 错误。

| 原始 sample | router 请求数 | 结果 |
| --- | ---: | --- |
| basic/01-simple-query.ts | 2 | 通过，Read 往返 |
| basic/02-multi-tool.ts | 2 | 通过，Glob/Bash 往返 |
| basic/03-multi-turn.ts | 7 | 通过，创建、读回、清理及历史续写 |
| basic/04-prompt-api.ts | 2 | 通过，Bash 查询 Node/npm 版本 |
| basic/05-custom-system-prompt.ts | 3 | 通过，读取 fixture 源码并回复 |
| tools/07-custom-tools.ts | 2 | 通过，两次天气工具与计算器 |
| advanced/13-hooks.ts | 2 | 通过，PreToolUse 与工具回传 |
| streaming/16-streaming.ts | 1 | 通过，流式 thinking/text |
| advanced/15-openai-compat.ts | 1 | 通过，OpenAI API 与 thinking |
| advanced/32-reasoning-effort.ts | 4 | 通过，low/medium/high/default |
| sessions/31-session-query-limit.ts | 9 | 通过，5 次 query 与压缩/历史 |
| streaming/17-streaming-with-tools.ts | 6 | 协议通过，两次 query success；Read、21 KB Write 参数、Bash/Grep 往返 |

其余需要 MCP 服务、搜索凭证、交互/Web 服务或专门 SDK 功能断言的 examples 未在本轮宣称通过；这不是完整 SDK examples 测试集全绿的声明。模型任务质量、其他供应商和所有参数组合仍不在本轮保证范围内。

完成运行共 41 次 router 请求。streaming/17 首次另有 5 次请求无 HTTP 错误，但触发验收器 180 秒超时，未算通过。停止遗留 Node 子进程后，改为直接启动 Node、时限 600 秒，重跑于 225 秒完成。

streaming/17 将文件写成工作目录内的 tool-module.ts，未遵守 prompt 指定的 /tmp/sdk-test-output.ts，也未执行 TypeScript 编译检查。因此该项只证明 SDK/router 流式工具链完成，不代表生成任务完全符合要求。

Go 全量测试、受影响包 race、vet、golangci-lint 均通过；SDK sample 四种组合另通过 race。临时 Key 文件已删除，原始输出日志不提交。
