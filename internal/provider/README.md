# 生成供应商

封装生成调用及流式结果，Bifrost Core 的类型、转换和配置细节限制在本模块内。仅接受路由核心已选的目标，不自主扩大候选、跨模型回退或执行工具。

SDK 的统一字段不代表供应商能力等价；对不支持的语义明确报错。客户端 SSE framing 属于 HTTP transport。当前使用锁定的 Bifrost Core；Anthropic Messages 客户端请求通过其 Responses/Chat 转换调用现有 OpenAI 兼容上游。prepared 数据按请求隔离，候选检查与发送使用相同转换路径；完整返回结果在此转换，不通过 routing 的精简响应类型。客户端 Anthropic 兼容入口不等于已接入原生 Anthropic 供应商。
