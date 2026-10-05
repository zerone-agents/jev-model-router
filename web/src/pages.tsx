import { useEffect, useState } from "react";
import {
  PageTitle,
  Empty,
  ErrorBox,
  Loading,
  NumberedPager,
  useRead,
  type PageProps,
} from "./components";
import { text } from "./i18n";
export type Model = {
  id: string;
  description: string;
  enabled: boolean;
  provider_id: string;
  upstream_name: string;
  location: string;
  capabilities: Record<string, unknown>;
  [key: string]: unknown;
};
export type Resource = { version: number; resource: Model };
export function Overview(props: PageProps) {
  const { lang } = props;
  const [refresh, setRefresh] = useState(0);
  const { data, error, loading } = useRead<{
    ready: boolean;
    version: number;
    enabled_models: number;
    records_degraded: boolean;
  }>(props, "status.get", {}, refresh);
  return (
    <>
      <PageTitle
        title={text(
          lang,
          "A clear view of your router.",
          "路由状态，一目了然。",
        )}
        description={text(
          lang,
          "One instance. Shared controls for people and agents.",
          "一个实例，Agent 与人类共用管理能力。",
        )}
      >
        <button onClick={() => setRefresh((x) => x + 1)}>
          {text(lang, "Refresh", "刷新")}
        </button>
      </PageTitle>
      {loading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} lang={lang} />
      ) : (
        data && (
          <>
            <section className="status-panel">
              <div className="eyebrow">
                {text(lang, "INSTANCE STATUS", "实例状态")}
              </div>
              <h2>
                <span className={"dot " + (data.ready ? "green" : "")} />
                {text(
                  lang,
                  data.ready ? "Ready" : "Needs configuration",
                  data.ready ? "已就绪" : "待配置",
                )}
              </h2>
              <p>
                {text(
                  lang,
                  "Configuration readiness, not an upstream health check.",
                  "表示配置就绪，不代表上游健康检查已通过。",
                )}
              </p>
              <div className="facts">
                <div>
                  <small>{text(lang, "Enabled models", "已启用模型")}</small>
                  <strong>{data.enabled_models}</strong>
                </div>
                <div>
                  <small>
                    {text(lang, "Configuration version", "配置版本")}
                  </small>
                  <strong>v{data.version}</strong>
                </div>
                <div>
                  <small>{text(lang, "Decision backend", "决策后端")}</small>
                  <strong>Jev</strong>
                </div>
              </div>
            </section>
            {data.records_degraded && (
              <div className="notice error">
                {text(
                  lang,
                  "Routing record storage is degraded. Generation can continue, but records may be missing.",
                  "路由记录存储已降级。生成仍可继续，但记录可能缺失。",
                )}
              </div>
            )}
            <section className="guidance">
              <h3>
                {text(
                  lang,
                  "Built for agents. Open to you.",
                  "Agent 优先，人类随时辅助。",
                )}
              </h3>
              <p>
                {text(
                  lang,
                  "Configure providers and models through the CLI. Use this workspace to refine descriptions, adjust routing preferences and inspect results.",
                  "通过 CLI 配置供应商和模型，在这里调整模型描述与路由偏好，查看调用结果。",
                )}
              </p>
              <code>jev-router schema</code>
              <code>jev-router schema models.put</code>
            </section>
          </>
        )
      )}
    </>
  );
}
export function Models(
  props: PageProps & {
    canEdit: boolean;
    hasUnsavedChanges?: () => boolean;
    onDirty: (dirty: boolean) => void;
    renderEditor?: (id: string) => React.ReactNode;
  },
) {
  const { lang } = props;
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [search, setSearch] = useState("");
  const query = search.trim();
  const highlight = (value: string) => (
    <SearchHighlight value={value} query={query} />
  );
  const [selected, setSelected] = useState("");
  const [listRevision, setListRevision] = useState(0);
  const { data, error, loading } = useRead<{
    items: Model[];
    total: number;
    next_cursor: string;
    version: number;
  }>(
    props,
    "models.list",
    {
      offset: (page - 1) * pageSize,
      limit: pageSize,
      sort: "enabled_first",
      ...(query ? { query, language: lang } : {}),
    },
    listRevision,
  );
  useEffect(() => {
    if (data?.total !== undefined) {
      const lastPage = Math.max(1, Math.ceil(data.total / pageSize));
      if (page > lastPage) setPage(lastPage);
    }
  }, [data?.total, page, pageSize]);
  return (
    <>
      <PageTitle
        title={text(lang, "Models", "模型")}
        description={text(
          lang,
          "Describe what each model does best. Capabilities stay explicit.",
          "用描述表达模型专长，用能力声明约束输入。",
        )}
      >
        {!selected && (
          <div className="model-list-actions">
            <input
              type="search"
              aria-label={text(lang, "Search models", "搜索模型")}
              placeholder={text(lang, "Search models…", "搜索模型…")}
              value={search}
              maxLength={256}
              onChange={(event) => {
                setSearch(event.target.value);
                setPage(1);
              }}
            />
            <button
              disabled={loading}
              onClick={() => {
                setPage(1);
                setListRevision((value) => value + 1);
              }}
            >
              {text(lang, "Refresh", "刷新")}
            </button>
          </div>
        )}
      </PageTitle>
      {selected ? (
        <>
          <button
            className="back"
            onClick={() => {
              if (
                !props.hasUnsavedChanges?.() ||
                window.confirm(
                  text(
                    lang,
                    "Return to the model list? Unsaved changes will be lost.",
                    "返回模型列表？未保存的修改将丢失。",
                  ),
                )
              ) {
                props.onDirty(false);
                setSelected("");
                setListRevision((value) => value + 1);
              }
            }}
          >
            ← {text(lang, "All models", "全部模型")}
          </button>
          {props.renderEditor ? (
            props.renderEditor(selected)
          ) : (
            <ModelRead {...props} id={selected} />
          )}
        </>
      ) : loading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} lang={lang} />
      ) : (
        data && (
          <>
            <div className="section-label">
              {text(lang, "MODEL REGISTRY", "模型列表")}{" "}
              <span>v{data.version}</span>
            </div>
            {!data.items.length ? (
              <Empty
                title={
                  query
                    ? text(lang, "No matching models", "没有匹配的模型")
                    : text(lang, "No models configured", "尚未配置模型")
                }
                detail={
                  query
                    ? text(
                        lang,
                        "Try another search or clear the search field.",
                        "请更换关键词或清空搜索框。",
                      )
                    : text(
                        lang,
                        "Use jev-router schema models.put to get started.",
                        "使用 jev-router schema models.put 开始配置。",
                      )
                }
              />
            ) : (
              <div className="model-list">
                {data.items.map((m) => (
                  <button
                    key={m.id}
                    className="model-row"
                    onClick={() => setSelected(m.id)}
                  >
                    <div className="model-monogram">
                      {m.id.slice(0, 1).toUpperCase()}
                    </div>
                    <div className="model-copy">
                      <h3>
                        {highlight(m.id)}
                        <span
                          className={
                            "badge " + (m.enabled ? "enabled" : "disabled")
                          }
                        >
                          {highlight(
                            text(
                              lang,
                              m.enabled ? "Enabled" : "Disabled",
                              m.enabled ? "已启用" : "已禁用",
                            ),
                          )}
                        </span>
                      </h3>
                      <p
                        className={
                          query &&
                          m.description
                            .toLowerCase()
                            .includes(query.toLowerCase())
                            ? "search-expanded"
                            : undefined
                        }
                      >
                        {highlight(
                          m.description ||
                            text(lang, "No description yet", "暂无描述"),
                        )}
                      </p>
                      <div className="model-details">
                        <small>
                          {highlight(`${m.provider_id} / ${m.upstream_name}`)}
                        </small>

                        {m.location && (
                          <span className="badge">{highlight(m.location)}</span>
                        )}
                        {m.capabilities.tools === true && (
                          <span className="badge">
                            {highlight(text(lang, "Tools", "工具"))}
                          </span>
                        )}
                        {m.capabilities.images === true && (
                          <span className="badge">
                            {highlight(text(lang, "Images", "图片"))}
                          </span>
                        )}
                        {m.capabilities.structured_output === true && (
                          <span className="badge">
                            {highlight(
                              text(lang, "Structured output", "结构化输出"),
                            )}
                          </span>
                        )}
                      </div>
                    </div>
                    <span aria-hidden>↗</span>
                  </button>
                ))}
              </div>
            )}
            <NumberedPager
              lang={lang}
              total={data.total ?? data.items.length}
              page={page}
              pageSize={pageSize}
              onPage={setPage}
              onPageSize={(size) => {
                setPageSize(size);
                setPage(1);
              }}
            />
          </>
        )
      )}
    </>
  );
}
export function ModelRead(props: PageProps & { id: string }) {
  const { data, error, loading } = useRead<Resource>(props, "models.get", {
    id: props.id,
  });
  return loading ? (
    <Loading />
  ) : error ? (
    <ErrorBox error={error} lang={props.lang} />
  ) : (
    <pre>{JSON.stringify(data?.resource, null, 2)}</pre>
  );
}
export function PromptRead(props: PageProps) {
  const { data, error, loading } = useRead<{ text: string; version: number }>(
    props,
    "prompt.get",
  );
  return (
    <>
      <PageTitle
        title={text(props.lang, "Routing prompt", "路由提示词")}
        description={text(
          props.lang,
          "One balanced template. Your routing preferences.",
          "一份均衡模板，表达你的选模偏好。",
        )}
      />
      {loading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} lang={props.lang} />
      ) : (
        <pre className="prompt-read">{data?.text}</pre>
      )}
    </>
  );
}
export function Records(props: PageProps) {
  const { lang } = props;
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [refresh, setRefresh] = useState(0);
  const { data, error, loading } = useRead<{
    records: Array<Record<string, unknown>>;
    next_cursor: string;
    total: number;
  }>(
    props,
    "records.list",
    { offset: (page - 1) * pageSize, limit: pageSize },
    refresh,
  );
  useEffect(() => {
    if (data?.total !== undefined) {
      const lastPage = Math.max(1, Math.ceil(data.total / pageSize));
      if (page > lastPage) setPage(lastPage);
    }
  }, [data?.total, page, pageSize]);
  return (
    <>
      <PageTitle
        title={text(lang, "Routing records", "路由记录")}
        description={text(
          lang,
          "Request excerpts and selection timing. Up to 120 characters of user text are retained.",
          "查看请求摘要与选模耗时，最多保留 120 字的用户消息文本。",
        )}
      >
        <button
          onClick={() => {
            setPage(1);
            setRefresh((x) => x + 1);
          }}
        >
          {text(lang, "Refresh", "刷新")}
        </button>
      </PageTitle>
      {loading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} lang={lang} />
      ) : data && !data.records.length ? (
        <Empty
          title={text(lang, "No routing records yet", "暂无路由记录")}
          detail={text(
            lang,
            "Requests sent through the inference API will appear here.",
            "通过推理 API 发起请求后，可在这里查看记录。",
          )}
        />
      ) : (
        data && (
          <>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    {[
                      text(lang, "Request / time", "请求 / 时间"),
                      text(lang, "Request summary", "请求摘要"),
                      text(lang, "Selected model", "选定模型"),
                      text(lang, "Path", "选择路径"),
                      text(lang, "Model selection time", "选模型耗时"),
                      text(lang, "Result", "结果"),
                    ].map((x) => (
                      <th key={x}>{x}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {data.records.map((r) => (
                    <tr key={String(r.request_id)}>
                      <td>
                        <details>
                          <summary className="request-id">
                            {String(r.request_id)}
                          </summary>
                          <pre>{JSON.stringify(r, null, 2)}</pre>
                        </details>
                        <small>{String(r.created_at)}</small>
                      </td>
                      <td className="request-summary">
                        <span className="request-summary-text">
                          {String(r.request_summary || "—")}
                        </span>
                      </td>
                      <td>{String(r.model_id || "—")}</td>
                      <td>
                        <span className="badge">{String(r.path || "—")}</span>
                      </td>
                      <td>
                        {typeof r.decision_ms === "number"
                          ? `${r.decision_ms} ms`
                          : "—"}
                      </td>
                      <td>
                        <span
                          className={
                            r.outcome === "success"
                              ? "record-result-success"
                              : r.outcome === "error"
                                ? "record-result-error"
                                : undefined
                          }
                        >
                          {r.outcome === "success"
                            ? text(lang, "Success", "成功")
                            : r.outcome === "error"
                              ? text(lang, "Failed", "失败")
                              : String(r.outcome || "—")}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <NumberedPager
              lang={lang}
              kind="records"
              total={data.total ?? data.records.length}
              page={page}
              pageSize={pageSize}
              onPage={setPage}
              onPageSize={(size) => {
                setPageSize(size);
                setPage(1);
              }}
            />
          </>
        )
      )}
    </>
  );
}

function SearchHighlight({ value, query }: { value: string; query: string }) {
  if (!query) return <>{value}</>;
  const lower = value.toLowerCase();
  const needle = query.toLowerCase();
  const parts: React.ReactNode[] = [];
  let position = 0;
  let match = lower.indexOf(needle);
  while (match !== -1) {
    parts.push(value.slice(position, match));
    parts.push(
      <mark key={match}>{value.slice(match, match + query.length)}</mark>,
    );
    position = match + query.length;
    match = lower.indexOf(needle, position);
  }
  parts.push(value.slice(position));
  return <>{parts}</>;
}
