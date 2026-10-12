import { replyErrorText } from "./errors";
import { ChoicePicker } from "../ui/ChoicePicker";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { ArrowUp, Stop, Plus, ArrowBendUpRight } from "@phosphor-icons/react";
import { APIError } from "../api";
import { ErrorBox, type PageProps } from "../components";
import { text } from "../i18n";
import {
  PlaygroundError,
  readPlaygroundStatus,
  streamPlayground,
  type PlaygroundStatus,
} from "./client";
import {
  history,
  initialState,
  isActive,
  reducePlayground,
  type Phase,
} from "./state";
import { ResponseBody } from "./ResponseBody";
import "./playground.css";
export function Playground({ client, lang, onError }: PageProps) {
  const [state, dispatch] = useReducer(
    reducePlayground,
    undefined,
    initialState,
  );
  const [model, setModel] = useState("auto"),
    [models, setModels] = useState<string[]>([]),
    [prompt, setPrompt] = useState("");
  const lifetime = useRef(new AbortController());
  const [modelsError, setModelsError] = useState<unknown>();
  const [modelsLoading, setModelsLoading] = useState(false);
  const [statusError, setStatusError] = useState<unknown>();
  const [statusLoading, setStatusLoading] = useState(false);
  const [sentBytes, setSentBytes] = useState<number | null>(null);
  const [status, setStatus] = useState<PlaygroundStatus>(),
    [error, setError] = useState<unknown>(),
    [until, setUntil] = useState(0),
    [now, setNow] = useState(Date.now()),
    [dayLimited, setDayLimited] = useState(false);
  const current = useRef<AbortController | null>(null),
    sequence = useRef(0),
    mounted = useRef(true),
    transcript = useRef<HTMLDivElement>(null);
  const active = isActive(state.phase),
    wait = Math.max(0, Math.ceil((until - now) / 1000));
  const refresh = useCallback(
    async (owner: AbortSignal) => {
      if (owner.aborted) return;
      setStatusLoading(true);
      setStatusError(undefined);
      const signal = AbortSignal.any([owner, AbortSignal.timeout(15000)]);
      try {
        const s = await readPlaygroundStatus(client, signal);
        if (!owner.aborted) setStatus(s);
      } catch (e) {
        if (!owner.aborted) {
          const failure =
            signal.reason?.name === "TimeoutError"
              ? new APIError("timeout")
              : e;
          setStatusError(failure);
          onError(failure);
        }
      } finally {
        if (!owner.aborted) setStatusLoading(false);
      }
    },
    [client, onError],
  );
  const refreshModels = useCallback(
    async (owner: AbortSignal) => {
      if (owner.aborted) return;
      setModelsLoading(true);
      setModelsError(undefined);
      let cursor = "",
        all: string[] = [];
      const seen = new Set<string>();
      try {
        do {
          const r = await client.call<{
            items: { id: string; enabled: boolean }[];
            next_cursor?: string;
          }>(
            "models.list",
            { input: { limit: 200, ...(cursor ? { cursor } : {}) } },
            owner,
          );
          all = all.concat(
            r.data.items.filter((m) => m.enabled).map((m) => m.id),
          );
          cursor = r.data.next_cursor || "";
          if (seen.has(cursor)) throw new APIError("invalid_response");
          seen.add(cursor);
        } while (cursor);
        if (!owner.aborted) setModels(all);
      } catch (e) {
        if (!owner.aborted) {
          setModelsError(e);
          onError(e);
        }
      } finally {
        if (!owner.aborted) setModelsLoading(false);
      }
    },
    [client, onError],
  );
  useEffect(() => {
    mounted.current = true;
    const abort = new AbortController();
    lifetime.current = abort;
    void refresh(abort.signal);
    void refreshModels(abort.signal);
    return () => {
      mounted.current = false;
      abort.abort();
      current.current?.abort();
      current.current = null;
      sequence.current++;
    };
  }, [client, onError, refresh, refreshModels]);
  useEffect(() => {
    if (until <= Date.now()) return;
    const timer = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(timer);
  }, [until]);
  useEffect(() => {
    const element = transcript.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [state]);
  const messages = history(state);
  const bytes = new TextEncoder().encode(
    messages.map((m) => m.content + (m.reasoning_content || "")).join("") +
      prompt,
  ).length;
  const tooLarge =
    !!status &&
    (bytes > status.limits.input_bytes ||
      messages.length + 1 > status.limits.max_messages);
  const stop = () => {
    const id = sequence.current;
    current.current?.abort();
    dispatch({ type: "stop", id });
    current.current = null;
  };
  const reset = () => {
    stop();
    sequence.current++;
    dispatch({ type: "reset" });
    setSentBytes(null);
    setPrompt("");
    setError(undefined);
  };
  const send = async () => {
    if (
      current.current ||
      !prompt.trim() ||
      !status?.enabled ||
      tooLarge ||
      wait
    )
      return;
    const id = ++sequence.current,
      abort = new AbortController(),
      input = prompt;
    current.current = abort;
    setSentBytes(bytes);
    setPrompt("");
    setError(undefined);
    setDayLimited(false);
    dispatch({ type: "start", id, prompt: input });
    try {
      await streamPlayground(
        client,
        {
          model,
          messages: [...messages, { role: "user", content: input }],
          stream: true,
        },
        abort.signal,
        (e) => {
          if (mounted.current && id === sequence.current)
            dispatch({ type: "event", id, event: e });
        },
      );
    } catch (e) {
      if (mounted.current && id === sequence.current) {
        dispatch({
          type: abort.signal.aborted ? "stop" : "fail",
          id,
          errorCode: e instanceof APIError ? e.code : "internal_error",
          limitScope: e instanceof PlaygroundError ? e.scope : undefined,
          diagnostic: e instanceof PlaygroundError ? e.diagnostic : undefined,
        });
        if (!abort.signal.aborted) {
          onError(e);
          if (e instanceof PlaygroundError) {
            setUntil(Date.now() + e.retryAfter * 1000);
            setNow(Date.now());
            setDayLimited(e.scope === "day");
          }
        }
      }
    } finally {
      if (mounted.current && id === sequence.current) {
        current.current = null;
        void refresh(lifetime.current.signal);
      }
    }
  };
  const label = (phase: Phase) => {
    const names: Record<Phase, [string, string]> = {
      idle: ["", ""],
      waiting: ["Waiting for response…", "等待响应…"],
      thinking: ["Thinking…", "正在思考…"],
      responding: ["", ""],
      completed: ["", ""],
      stopped: ["Stopped", "已停止"],
      failed: ["Failed or interrupted", "失败或中断"],
      truncated: ["Output limit reached", "已达到输出上限"],
    };
    return text(lang, ...names[phase]);
  };
  const route = state.turns.at(-1)?.route;
  return (
    <section className="pg-page">
      <header className="page-heading">
        <div>
          <h1>Playground</h1>
          <p>
            {text(
              lang,
              "A conversation. The right model for each turn.",
              "发起对话，体验每一轮的模型选择。",
            )}
          </p>
        </div>
        <button onClick={reset}>
          <Plus size={16} />
          {text(lang, "New conversation", "新建对话")}
        </button>
      </header>
      <div className="pg-layout">
        <div className="pg-conversation">
          <div className="pg-toolbar">
            <span>{text(lang, "Model", "模型")}</span>
            <ChoicePicker
              className="pg-model-picker"
              label={text(lang, "Model", "模型")}
              value={model}
              disabled={active}
              onChange={setModel}
              items={[
                {
                  value: "auto",
                  label: text(
                    lang,
                    "Auto · let the router choose",
                    "Auto · 自动选模",
                  ),
                },
                ...models.map((id) => ({ value: id, label: id })),
              ]}
            />
          </div>
          {modelsLoading && (
            <p role="status">
              {text(lang, "Loading models…", "正在读取模型…")}
            </p>
          )}
          {modelsError != null && (
            <div>
              <ErrorBox error={modelsError} lang={lang} />
              <button
                type="button"
                disabled={modelsLoading}
                onClick={() => void refreshModels(lifetime.current.signal)}
              >
                {text(lang, "Retry models", "重试读取模型")}
              </button>
            </div>
          )}
          <div
            className="pg-transcript"
            ref={transcript}
            aria-label={text(lang, "Conversation", "对话")}
          >
            {!state.turns.length && (
              <div className="pg-empty">
                <ArrowBendUpRight size={32} weight="light" />
                <h2>
                  {text(
                    lang,
                    "Try a task. See where it goes.",
                    "试一个任务，看看它会交给谁。",
                  )}
                </h2>
                <p>
                  {text(
                    lang,
                    "Start with a question, a draft, or a problem to solve. Auto selects from your enabled models.",
                    "输入问题、文案或待解决的难题。Auto 会从已启用的模型中选择。",
                  )}
                </p>
              </div>
            )}
            {state.turns.map((t) => (
              <article className="pg-turn" key={t.id}>
                <div className="pg-user">
                  <p>{t.prompt}</p>
                </div>
                <div className="pg-answer">
                  <small>
                    {t.route?.model_id || text(lang, "Router", "路由器")}
                  </small>
                  {t.content && (
                    <ResponseBody
                      content={t.content}
                      streaming={isActive(t.phase)}
                    />
                  )}
                  {t.phase === "failed" && (
                    <div className="pg-reply-error" role="alert">
                      {t.errorCode
                        ? replyErrorText(t.errorCode, t.limitScope, lang)
                        : label(t.phase)}
                      {t.diagnostic?.stage && (
                        <div>
                          {text(lang, "Stage", "失败阶段")}:{" "}
                          {t.diagnostic.stage === "generation"
                            ? text(lang, "Generation", "生成")
                            : t.diagnostic.stage === "routing"
                              ? text(lang, "Routing", "选模")
                              : text(lang, "Request", "请求")}
                        </div>
                      )}
                      {t.diagnostic?.upstream_status && (
                        <div>HTTP {t.diagnostic.upstream_status}</div>
                      )}
                      {t.diagnostic?.upstream_code && (
                        <div>{t.diagnostic.upstream_code}</div>
                      )}
                      {t.diagnostic?.upstream_error && (
                        <details className="pg-upstream-details">
                          <summary>
                            {text(
                              lang,
                              "Upstream error details",
                              "上游错误详情",
                            )}
                          </summary>
                          {t.diagnostic.upstream_error.request_id && (
                            <div>
                              {text(lang, "Upstream request ID", "上游请求 ID")}
                              : {t.diagnostic.upstream_error.request_id}
                            </div>
                          )}
                          <pre>
                            {typeof t.diagnostic.upstream_error.body ===
                            "string"
                              ? t.diagnostic.upstream_error.body
                              : JSON.stringify(
                                  t.diagnostic.upstream_error.body,
                                  null,
                                  2,
                                )}
                          </pre>
                          {t.diagnostic.upstream_error.truncated && (
                            <div>
                              {text(
                                lang,
                                "Details truncated due to size limit.",
                                "详情超出长度限制，已截断。",
                              )}
                            </div>
                          )}
                        </details>
                      )}
                      {(t.diagnostic?.request_id || t.route?.request_id) && (
                        <div>
                          {text(lang, "Request ID", "请求 ID")}:{" "}
                          {t.diagnostic?.request_id || t.route?.request_id}
                        </div>
                      )}
                    </div>
                  )}
                  {t.phase !== "failed" && label(t.phase) && (
                    <div
                      className={`pg-phase ${isActive(t.phase) ? "pg-pending" : ""}`}
                      role="status"
                    >
                      {label(t.phase)}
                    </div>
                  )}
                  {t.usage && (
                    <small className="muted">
                      {text(lang, "Tokens", "Tokens")}: {t.usage.input_tokens} →{" "}
                      {t.usage.output_tokens}
                    </small>
                  )}
                </div>
              </article>
            ))}
          </div>
          <form
            className="pg-compose"
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
          >
            <label className="sr-only" htmlFor="pg-message">
              {text(lang, "Message", "消息")}
            </label>
            <textarea
              id="pg-message"
              value={prompt}
              onChange={(e) => {
                setSentBytes(null);
                setPrompt(e.target.value);
              }}
              onKeyDown={(e) => {
                if (
                  e.key === "Enter" &&
                  !e.shiftKey &&
                  !e.nativeEvent.isComposing &&
                  e.nativeEvent.keyCode !== 229
                ) {
                  e.preventDefault();
                  if (!e.repeat) void send();
                }
              }}
              aria-describedby="pg-keyboard-hint"
              placeholder={text(lang, "Ask something…", "输入你的任务…")}
              rows={3}
              disabled={active}
            />
            <div className="pg-compose-bottom">
              <div className="pg-compose-info">
                <p id="pg-keyboard-hint" className="pg-keyboard-hint">
                  {text(
                    lang,
                    "Enter to send · Shift+Enter for a new line",
                    "Enter 发送 · Shift+Enter 换行",
                  )}
                </p>
                <small className={tooLarge ? "pg-danger" : "muted"}>
                  {status
                    ? `${(sentBytes ?? bytes).toLocaleString()} / ${status.limits.input_bytes.toLocaleString()} bytes`
                    : statusError != null
                      ? text(lang, "Limits unavailable", "限额读取失败")
                      : text(lang, "Loading limits…", "正在读取限额…")}
                </small>
              </div>
              {active ? (
                <button type="button" onClick={stop}>
                  <Stop size={16} />
                  {text(lang, "Stop", "停止")}
                </button>
              ) : (
                <button
                  className="primary"
                  disabled={
                    !prompt.trim() || !status?.enabled || tooLarge || wait > 0
                  }
                  type="submit"
                >
                  <ArrowUp size={16} />
                  {wait ? `${wait}s` : text(lang, "Send", "发送")}
                </button>
              )}
            </div>
            {tooLarge && (
              <p className="pg-danger">
                {text(
                  lang,
                  "Conversation limit reached. Start a new conversation or shorten your message.",
                  "对话已超出限制，请新建对话或缩短消息。",
                )}
              </p>
            )}
          </form>
          <p className="pg-notice">
            {text(
              lang,
              "Uses real models and may incur charges. Conversations stay in this page and are lost on refresh.",
              "会调用真实模型并可能产生费用。对话仅保存在当前页面，刷新后丢失。",
            )}
          </p>
          {statusError != null && (
            <div>
              <ErrorBox error={statusError} lang={lang} />
              <button
                type="button"
                disabled={statusLoading}
                onClick={() => void refresh(lifetime.current.signal)}
              >
                {text(lang, "Retry limits", "重试读取限额")}
              </button>
            </div>
          )}
          {error != null && <ErrorBox error={error} lang={lang} />}{" "}
          {dayLimited && (
            <p role="status">
              {text(
                lang,
                "Daily allowance reached. Resets at ",
                "今日额度已用尽，重置时间：",
              )}
              {status &&
                new Date(status.quota.reset_at).toLocaleString(
                  lang === "zh" ? "zh-CN" : "en-US",
                )}
            </p>
          )}
          {status && !status.enabled && (
            <p role="status">
              {text(
                lang,
                "Playground is disabled on this instance.",
                "此实例已关闭 Playground。",
              )}
            </p>
          )}
        </div>
        <aside className="pg-inspector">
          <div className="pg-detail">
            <h2>{text(lang, "This route", "本次路由")}</h2>
            {route ? (
              <dl>
                <dt>{text(lang, "Selected model", "选定模型")}</dt>
                <dd>{route.model_id}</dd>
                <dt>{text(lang, "Selection path", "选择路径")}</dt>
                <dd>
                  {{
                    explicit: text(lang, "Selected manually", "手动指定"),
                    single_candidate: text(
                      lang,
                      "Only eligible model",
                      "唯一合格模型",
                    ),
                    decision: text(lang, "Selected by Jev", "Jev 选模"),
                    context_estimate_fallback: text(
                      lang,
                      "Largest context fallback",
                      "最大上下文保底",
                    ),
                  }[route.path] || route.path}
                </dd>
                <dt>{text(lang, "Routing time", "选模耗时")}</dt>
                <dd>{route.decision_ms} ms</dd>
                <dt>{text(lang, "Configuration", "配置版本")}</dt>
                <dd>v{route.config_version}</dd>
                <dt>{text(lang, "Request ID", "请求 ID")}</dt>
                <dd className="pg-request-id">{route.request_id}</dd>
              </dl>
            ) : (
              <p className="muted">
                {text(
                  lang,
                  "Send a message to see the actual routing result.",
                  "发送消息后，查看实际路由结果。",
                )}
              </p>
            )}
          </div>
          <div className="pg-detail">
            <h2>{text(lang, "Playground allowance", "体验额度")}</h2>
            {status ? (
              <>
                <div className="pg-allowance">
                  <strong>{status.quota.day_remaining}</strong>
                  <span>
                    / {status.limits.daily_requests}{" "}
                    {text(lang, "today", "今日")}
                  </span>
                </div>
                <p className="muted">
                  {text(
                    lang,
                    "Shared across this instance. Failed and stopped requests also count.",
                    "由整个实例共享。已受理的失败与停止请求也计入额度。",
                  )}
                </p>
                <dl>
                  <dt>{text(lang, "Per session / minute", "每会话 / 分钟")}</dt>
                  <dd>{status.limits.session_rpm}</dd>
                  <dt>{text(lang, "Instance / minute", "实例 / 分钟")}</dt>
                  <dd>{status.limits.instance_rpm}</dd>
                  <dt>{text(lang, "Output limit", "输出上限")}</dt>
                  <dd>{status.limits.output_tokens} tokens</dd>
                </dl>
              </>
            ) : (
              <p className="muted">{text(lang, "Loading…", "加载中…")}</p>
            )}
          </div>
        </aside>
      </div>
    </section>
  );
}
