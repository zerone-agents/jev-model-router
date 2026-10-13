import { useEffect, useEffectEvent, useRef, useState } from "react";
import { type Lang, text } from "./i18n";
import { type PageProps, ErrorBox } from "./components";
import { APIError } from "./api";
export type ModelHealth = {
  state: "unknown" | "healthy" | "isolated";
  last_success_at: string | null;
  last_failure_at: string | null;
  next_probe_at: string | null;
  reason?: string;
  message?: string;
  upstream_status?: number;
  upstream_code?: string;
};
export function HealthBadge({
  health,
  lang,
}: {
  health?: ModelHealth;
  lang: Lang;
}) {
  const labels = {
    unknown: ["Unknown", "未知"],
    healthy: ["Healthy", "健康"],
    isolated: ["Isolated", "已隔离"],
  };
  const label = health
    ? labels[health.state]
    : ["Health unavailable", "健康状态不可用"];
  return (
    <span className={`badge health-${health?.state || "unavailable"}`}>
      {text(lang, label[0], label[1])}
    </span>
  );
}
export function HealthDetails(
  props: PageProps & {
    id: string;
    health?: ModelHealth;
    canTest: boolean;
    onHealth: (h: ModelHealth | undefined) => void;
  },
) {
  const { client, lang, id, onError, onHealth } = props;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState("");
  const active = useRef<AbortController | null>(null);
  const probing = useRef(false);
  const refresh = useEffectEvent(() => void check(false));
  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    const timer = window.setInterval(visible, 15000);
    document.addEventListener("visibilitychange", visible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [id, client]);
  useEffect(
    () => () => {
      active.current?.abort();
      active.current = null;
    },
    [id],
  );
  async function check(probe: boolean) {
    if (active.current) {
      if (!probe || probing.current) return;
      active.current.abort();
    }
    probing.current = probe;
    const c = new AbortController();
    active.current = c;
    if (probe) {
      setBusy(true);
      setError(undefined);
      setNotice("");
    }
    try {
      if (probe) {
        const r = await client.call<{
          ok: boolean;
          error?: { code: string };
          health?: ModelHealth;
          persisted?: boolean;
        }>(
          "models.test",
          { input: { id } },
          AbortSignal.any([c.signal, AbortSignal.timeout(120000)]),
        );
        if (c.signal.aborted) return;
        onHealth(r.data.health);
        if (!r.data.ok)
          setError(new APIError(r.data.error?.code || "upstream_error"));
        if (r.data.persisted === false)
          setNotice(
            text(
              lang,
              "Health update could not be saved. Retry the check.",
              "健康状态未能保存，请重试检测。",
            ),
          );
        else if (r.data.ok)
          setNotice(
            text(lang, "Connection check succeeded.", "连接检测成功。"),
          );
      }
      const r = await client.call<{ resource: { health?: ModelHealth } }>(
        "models.get",
        { input: { id } },
        AbortSignal.any([c.signal, AbortSignal.timeout(15000)]),
      );
      if (!c.signal.aborted) onHealth(r.data.resource.health);
    } catch (e) {
      if (!c.signal.aborted) {
        if (probe) setError(e);
        onError(e);
      }
    } finally {
      if (active.current === c) {
        active.current = null;
        setBusy(false);
      }
    }
  }
  const h = props.health;
  const date = (v: string | null | undefined) =>
    v ? new Date(v).toLocaleString(lang === "zh" ? "zh-CN" : "en-US") : "—";
  return (
    <section
      className="health-details"
      aria-label={text(lang, "Upstream health", "上游健康")}
    >
      <div className="health-summary">
        <dl>
          <div>
            <dt>{text(lang, "Last success", "最近成功")}</dt>
            <dd>{date(h?.last_success_at)}</dd>
          </div>
          <div>
            <dt>{text(lang, "Last failure", "最近失败")}</dt>
            <dd>{date(h?.last_failure_at)}</dd>
          </div>
          <div>
            <dt>{text(lang, "Next background check", "下次后台检测")}</dt>
            <dd>{date(h?.next_probe_at)}</dd>
          </div>
        </dl>
        {props.canTest && (
          <button disabled={busy} onClick={() => void check(true)}>
            {busy
              ? text(lang, "Checking…", "检测中…")
              : text(lang, "Check now", "立即检测")}
          </button>
        )}
      </div>
      {h?.reason && (
        <p className="health-reason">
          {text(
            lang,
            h.message || h.reason,
            h.reason === "rate_limited"
              ? "上游暂时限流。"
              : h.reason === "configuration_or_quota"
                ? "上游凭证、额度或模型配置不可用。"
                : "上游连接或服务暂时不可用。",
          )}
          {h.upstream_status ? ` · HTTP ${h.upstream_status}` : ""}
          {h.upstream_code ? ` · ${h.upstream_code}` : ""}
        </p>
      )}
      {error !== undefined && <ErrorBox error={error} lang={lang} />}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
