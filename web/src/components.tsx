import { ChoicePicker } from "./ui/ChoicePicker";
import { Fragment, useEffect, useState } from "react";
import { APIError, type Client } from "./api";
import { text, type Lang } from "./i18n";
export type PageProps = {
  client: Client;
  lang: Lang;
  onError: (e: unknown) => void;
};
export function errorText(e: unknown, lang: Lang) {
  const code = e instanceof APIError ? e.code : "";
  const map: Record<string, [string, string]> = {
    playground_rate_limited: [
      "Playground limit reached. Wait before sending again.",
      "已达到 Playground 限额，请稍后再发送。",
    ],
    request_too_large: [
      "Conversation exceeds the Playground limit.",
      "对话超出 Playground 限制。",
    ],
    interrupted: [
      "The response was interrupted. Partial output is not added to the next turn.",
      "响应已中断，部分输出不会加入下一轮上下文。",
    ],
    playground_unavailable: [
      "Playground is unavailable on this instance.",
      "此实例的 Playground 暂不可用。",
    ],
    https_required: [
      "Use HTTPS to connect to a remote instance.",
      "连接远程实例需要 HTTPS。",
    ],
    retry_expired: [
      "The retry window has expired. Read the latest configuration to reconcile.",
      "已超过重试期限，请读取最新配置核对。",
    ],
    unauthorized: [
      "Credential expired or invalid. Reconnect to continue.",
      "凭证无效或已过期，请重新连接。",
    ],
    session_changed: [
      "Session changed in another tab. Restore it before deciding what to do next.",
      "另一标签页已更换会话，请恢复会话后再决定操作。",
    ],
    session_limit: [
      "Session capacity reached. Log out an existing session or wait for expiry.",
      "会话数量已达上限，请注销已有会话或等待过期。",
    ],
    forbidden: [
      "This credential cannot manage this instance.",
      "此凭证没有管理权限。",
    ],
    config_conflict: [
      "Configuration changed. Read the latest version before saving again.",
      "配置已变化，请读取最新版本后再保存。",
    ],
    invalid_request: [
      "Input does not match the instance contract.",
      "输入不符合实例契约。",
    ],
    timeout: ["Request timed out. Please try again.", "请求超时，请重试。"],
    network_error: [
      "No response received. Check the connection.",
      "未收到响应，请检查连接。",
    ],
    invalid_response: [
      "The instance returned an unreadable response.",
      "实例返回了无法解析的响应。",
    ],
  };
  const pair = map[code] || [
    "Request failed. Please check the instance and try again.",
    "请求失败，请检查实例后重试。",
  ];
  return text(lang, ...pair);
}
export function ErrorBox({ error, lang }: { error: unknown; lang: Lang }) {
  return (
    <div className="notice error" role="alert">
      {errorText(error, lang)}
      {error instanceof APIError && error.operationId && (
        <small>
          {text(lang, "Operation", "操作")} · {error.operationId}
        </small>
      )}
    </div>
  );
}
export function useRead<T>(
  { client, onError }: PageProps,
  id: string,
  input: unknown = {},
  refresh = 0,
) {
  const [state, setState] = useState<{
    data?: T;
    error?: unknown;
    loading: boolean;
  }>({ loading: true });
  const key = JSON.stringify(input);
  useEffect(() => {
    let active = true;
    const abort = new AbortController();
    setState({ loading: true });
    client
      .call<T>(id, { input: JSON.parse(key) }, abort.signal)
      .then((r) => {
        if (active) setState({ data: r.data, loading: false });
      })
      .catch((e) => {
        if (active) {
          setState({ error: e, loading: false });
          onError(e);
        }
      });
    return () => {
      active = false;
      abort.abort();
    };
  }, [client, id, key, refresh, onError]);
  return state;
}
export function Loading() {
  return (
    <div className="skeleton" role="status" aria-label="Loading">
      <span />
      <span />
      <span />
    </div>
  );
}
export function PageTitle({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children?: React.ReactNode;
}) {
  return (
    <header className="page-title">
      <div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {children}
    </header>
  );
}
export function Empty({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="empty">
      <span className="empty-line" />
      <h3>{title}</h3>
      <p>{detail}</p>
    </div>
  );
}
export function Pager({
  next,
  back,
  page,
  onNext,
  onBack,
  lang,
}: {
  next: string;
  back: boolean;
  page?: number;
  onNext: () => void;
  onBack: () => void;
  lang: Lang;
}) {
  return (
    <div className="pager">
      {page !== undefined && (
        <span>{text(lang, `Page ${page}`, `第 ${page} 页`)}</span>
      )}
      <button disabled={!back} onClick={onBack}>
        {text(lang, "Previous", "上一页")}
      </button>
      <button disabled={!next} onClick={onNext}>
        {text(lang, "Next", "下一页")}
      </button>
    </div>
  );
}

export function NumberedPager({
  lang,
  kind = "models",
  total,
  page,
  pageSize,
  onPage,
  onPageSize,
}: {
  lang: Lang;
  kind?: "models" | "records";
  total: number;
  page: number;
  pageSize: number;
  onPage: (page: number) => void;
  onPageSize: (size: number) => void;
}) {
  const count = Math.max(1, Math.ceil(total / pageSize));
  const start = Math.max(1, Math.min(page - 2, count - 4));
  const visible = [
    ...new Set([
      1,
      ...Array.from({ length: Math.min(5, count) }, (_, i) => start + i),
      count,
    ]),
  ].sort((a, b) => a - b);
  return (
    <nav
      className="numbered-pager"
      aria-label={text(
        lang,
        kind === "models" ? "Model pagination" : "Record pagination",
        kind === "models" ? "模型分页" : "记录分页",
      )}
    >
      <span className="pagination-total">
        {kind === "models"
          ? text(lang, `${total} models`, `共 ${total} 个模型`)
          : text(lang, `${total} records`, `共 ${total} 条记录`)}
      </span>
      <div className="page-numbers">
        <button
          aria-label={text(lang, "Previous", "上一页")}
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
        >
          ‹
        </button>
        {visible.map((value, index) => (
          <Fragment key={value}>
            {index > 0 && value - visible[index - 1] > 1 && (
              <span className="page-ellipsis" aria-hidden="true">
                ···
              </span>
            )}
            <button
              aria-label={text(lang, `Page ${value}`, `第 ${value} 页`)}
              aria-current={page === value ? "page" : undefined}
              onClick={() => onPage(value)}
            >
              {value}
            </button>
          </Fragment>
        ))}
        <button
          aria-label={text(lang, "Next", "下一页")}
          disabled={page >= count}
          onClick={() => onPage(page + 1)}
        >
          ›
        </button>
      </div>
      <PageSizePicker
        kind={kind}
        lang={lang}
        value={pageSize}
        onChange={onPageSize}
      />
    </nav>
  );
}

function PageSizePicker({
  lang,
  kind,
  value,
  onChange,
}: {
  lang: Lang;
  kind: "models" | "records";
  value: number;
  onChange: (size: number) => void;
}) {
  return (
    <ChoicePicker
      className="page-size-picker"
      placement="top"
      label={text(
        lang,
        kind === "models" ? "Models per page" : "Records per page",
        kind === "models" ? "每页模型数" : "每页记录数",
      )}
      value={String(value)}
      onChange={(value) => onChange(Number(value))}
      items={[10, 20, 50, 100].map((size) => ({
        value: String(size),
        label: text(lang, `${size} / page`, `${size} 条 / 页`),
      }))}
    />
  );
}
