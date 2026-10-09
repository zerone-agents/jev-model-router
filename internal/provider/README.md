# 生成供应商

封装生成调用及流式结果，Bifrost Core 的类型、转换和配置细节限制在本模块内。仅接受路由核心已选的目标，不自主扩大候选、跨模型回退或执行工具。

SDK 的统一字段不代表供应商能力等价；对不支持的语义明确报错。客户端 SSE framing 属于 HTTP transport。当前使用锁定的 Bifrost Core；Anthropic Messages 客户端请求通过其 Responses/Chat 转换调用现有 OpenAI 兼容上游。prepared 数据按请求隔离，候选检查与发送使用相同转换路径；完整返回结果在此转换，不通过 routing 的精简响应类型。OpenAI Chat 入口可选择 protocol=anthropic 的原生生成供应商；客户端 Messages 入口到原生 Anthropic 的方向尚未实现，由 #51 追踪。

原生上游 SSE 在请求级 SSEReaderFactory 中先做有界 framing 和状态校验，再交给 Bifrost 转换。message_stop 在响应体 EOF 前不交给 SDK；输出包装器扣住 finish_reason，直到校验完成且 SDK 通道正常关闭。总读取上限 16 MiB、单帧 1 MiB，取消和空闲超时会失败并释放连接。RawResponse 仅用于 JSON 完整性核验，不能代替 SSE 解析前检查。
