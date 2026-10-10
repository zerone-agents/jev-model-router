import { APIError } from "../api";
import { errorText } from "../components";
import { text, type Lang } from "../i18n";

export function replyErrorText(
  code: string,
  scope: string | undefined,
  lang: Lang,
) {
  if (code === "no_candidates")
    return text(
      lang,
      "No enabled model supports this conversation's content or history. Check model capabilities and upstream protocol compatibility.",
      "没有已启用的模型能够处理此对话的内容或历史，请检查模型能力及上游协议兼容性。",
    );
  if (code === "unsupported_request")
    return text(
      lang,
      "The selected model or upstream protocol does not support this request or conversation history.",
      "所选模型或上游协议不支持此请求或对话历史。",
    );
  if (code === "playground_rate_limited") {
    const messages: Record<string, [string, string]> = {
      session_minute: [
        "This session's per-minute request limit has been reached. Wait for the countdown before sending again.",
        "本会话每分钟请求数已超限，请等待倒计时结束后再发送。",
      ],
      instance_minute: [
        "This instance's per-minute request limit has been reached. Wait for the countdown before sending again.",
        "此实例每分钟请求数已超限，请等待倒计时结束后再发送。",
      ],
      day: [
        "This instance's daily Playground allowance has been exhausted. It resets at 00:00 UTC.",
        "此实例今日 Playground 额度已用尽，将于 UTC 00:00 重置。",
      ],
      session_concurrency: [
        "This session has too many requests in progress. Wait for a reply to finish or stop it before sending again.",
        "本会话同时进行的请求过多，请等待当前回复结束或停止后再发送。",
      ],
      instance_concurrency: [
        "This instance has too many requests in progress. Wait for an active request to finish before sending again.",
        "此实例同时进行的请求过多，请等待进行中的请求结束后再发送。",
      ],
    };
    if (scope && messages[scope]) return text(lang, ...messages[scope]);
  }
  return errorText(new APIError(code), lang);
}
