# 生成供应商

封装生成调用及流式结果，Bifrost Core 的类型、转换和配置细节限制在本模块内。仅接受路由核心已选的目标，不自主扩大候选、跨模型回退或执行工具。

SDK 的统一字段不代表供应商能力等价；不支持的语义明确报错。PreparedMessages 保存完整不可变原生 JSON。Projection 仅用于决策，Estimate 独立统计完整输入，Check 按目标选择 nativeBody 或 chatRequest，实际执行不发送规划投影。原生 Messages 复用锁定 Bifrost Passthrough/PassthroughStream，只替换 model，保留签名、redacted、cache、工具数值及原生 usage；OpenAI 目标继续既有转换。

原生 SSE 共享有界 framing，各客户端使用独立验证状态；Chat 的规则不继承 Messages max_tokens 宽容策略。工具参数在块结束时分类，最终停止原因决定是否允许最后工具块的未完成对象前缀。message_stop 等到干净 EOF；压缩及解压各16 MiB、帧1 MiB、65536帧。取消/超时优先，Close 幂等且只释放一次 lease。错误按安全映射重新编码，不能透传上游诊断。详见 ../../docs/compatibility.md。
