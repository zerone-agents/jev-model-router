import { useCallback, useEffect, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  SquaresFour,
  Stack,
  SlidersHorizontal,
  ListBullets,
  SignOut,
  Translate,
  List,
  X,
  ArrowRight,
} from "@phosphor-icons/react";
import { Session } from "./session";
import { APIError, secureOrigin, type Client } from "./api";
import { labels, text, type Lang } from "./i18n";
import { ErrorBox } from "./components";
import { Overview, Models, Records } from "./pages";
import { ModelEditor, PromptEditor } from "./editors";
type Page = keyof typeof labels.en;
const required: Record<Page, string> = {
  overview: "status.get",
  models: "models.list",
  prompt: "prompt.get",
  records: "records.list",
};
const icons = {
  overview: SquaresFour,
  models: Stack,
  prompt: SlidersHorizontal,
  records: ListBullets,
};
function initialLang(): Lang {
  try {
    const saved = localStorage.getItem("jev-language");
    if (saved === "en" || saved === "zh") return saved;
  } catch {}
  return navigator.language.startsWith("zh") ? "zh" : "en";
}
export function App() {
  const [session] = useState(() => new Session());
  const [client, setClient] = useState<Client | null>(null);
  const [lang, setLang] = useState<Lang>(initialLang);
  const [page, setPage] = useState<Page>("overview");
  const [token, setToken] = useState("");
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [mobile, setMobile] = useState(false);
  const dirty = useRef(false);
  const setDirty = useCallback((value: boolean) => {
    dirty.current = value;
  }, []);
  const disconnect = useCallback(() => {
    session.disconnect();
    setClient(null);
    setToken("");
    setBusy(false);
    dirty.current = false;
    setMobile(false);
    setError(undefined);
  }, [session]);
  useEffect(() => () => session.disconnect(), [session]);
  useEffect(() => {
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    try {
      localStorage.setItem("jev-language", lang);
    } catch {}
  }, [lang]);
  const onError = useCallback(
    (e: unknown) => {
      if (
        e instanceof APIError &&
        e.status === 401 &&
        session.client === client
      ) {
        disconnect();
        setError(e);
      }
    },
    [client, disconnect, session],
  );
  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => {
      if (dirty.current) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, []);
  const navigate = (next: Page) => {
    if (next === page) return;
    if (
      dirty.current &&
      !window.confirm(
        text(lang, "Discard unsaved changes?", "放弃未保存的修改？"),
      )
    )
      return;
    dirty.current = false;
    setPage(next);
    setMobile(false);
  };
  const can = (id: string) => session.capabilities.some((c) => c.id === id);
  const nav = (
    <>
      <a
        className="brand"
        href="#"
        onClick={(e) => {
          e.preventDefault();
          navigate("overview");
        }}
      >
        <img src="/dashboard/zerone.svg" alt="Zerone" />
        <div>
          Jev Router<small>ZERONE</small>
        </div>
      </a>
      <div className="nav-caption">{text(lang, "WORKSPACE", "工作区")}</div>
      <nav>
        {(Object.keys(labels.en) as Page[]).map((p) => {
          const Icon = icons[p];
          return (
            <button
              key={p}
              className={page === p ? "selected" : ""}
              aria-current={page === p ? "page" : undefined}
              onClick={() => navigate(p)}
            >
              <Icon size={20} />
              {labels[lang][p]}
            </button>
          );
        })}
      </nav>
      <div className="sidebar-bottom">
        <span className="dot green" />
        {text(lang, "Connected to instance", "已连接实例")}
        <small>{location.host}</small>
        <button
          onClick={() => {
            if (
              !dirty.current ||
              window.confirm(
                text(
                  lang,
                  "Disconnect and discard unsaved changes?",
                  "断开连接并放弃未保存的修改？",
                ),
              )
            )
              disconnect();
          }}
        >
          <SignOut size={18} />
          {text(lang, "Disconnect", "断开连接")}
        </button>
      </div>
    </>
  );
  const props = client ? { client, lang, onError } : null;
  return (
    <div className="app">
      <button
        className="language"
        onClick={() => setLang(lang === "en" ? "zh" : "en")}
        aria-label={text(lang, "Switch language", "切换语言")}
      >
        <Translate size={17} />
        {lang === "en" ? "简体中文" : "English"}
      </button>
      {!client ? (
        <div className="connect-layout">
          <div className="connect-brand">
            <img src="/dashboard/zerone.svg" alt="Zerone" />
            <span>ZERONE / JEV ROUTER</span>
          </div>
          <section className="connect-card">
            <div className="eyebrow">
              {text(lang, "YOUR ROUTING WORKSPACE", "你的路由工作区")}
            </div>
            <h1>
              {text(
                lang,
                "A little guidance.\nBetter routing.",
                "轻松调整，\n让路由更合适。",
              )}
            </h1>
            <p>
              {text(
                lang,
                "Connect to your instance to refine model descriptions and routing preferences.",
                "连接你的实例，调整模型描述与选模偏好。",
              )}
            </p>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                if (busy) return;
                if (!secureOrigin(new URL(location.href))) {
                  setError(new APIError("https_required"));
                  return;
                }
                setBusy(true);
                setError(undefined);
                const value = token;
                setToken("");
                try {
                  if (await session.connect(value)) {
                    setClient(session.client);
                    setPage("overview");
                  }
                } catch (e) {
                  setError(e);
                } finally {
                  setBusy(false);
                }
              }}
            >
              <label htmlFor="credential">
                {text(lang, "Settings credential", "Settings 凭证")}
              </label>
              <input
                id="credential"
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={token}
                onChange={(e) => setToken(e.target.value)}
                required
                disabled={busy}
              />
              <small>
                {text(
                  lang,
                  "Kept in memory only. Refreshing requires reconnecting.",
                  "凭证仅保留在页面内存中，刷新后需重新连接。",
                )}
              </small>
              {error != null && <ErrorBox error={error} lang={lang} />}
              <button className="primary" disabled={busy || !token}>
                {text(
                  lang,
                  busy ? "Connecting…" : "Connect to instance",
                  busy ? "正在连接…" : "连接实例",
                )}
                <ArrowRight size={18} />
              </button>
            </form>
            <div className="connect-host">{location.host}</div>
          </section>
          <footer>
            {text(
              lang,
              "Agent-first controls. Human perspective.",
              "Agent 优先的能力，人类可见的视角。",
            )}
          </footer>
        </div>
      ) : (
        <>
          <aside className="sidebar">{nav}</aside>
          <Dialog.Root open={mobile} onOpenChange={setMobile}>
            <Dialog.Trigger asChild>
              <button
                className="mobile-menu"
                aria-label={text(lang, "Open navigation", "打开导航")}
              >
                <List size={24} />
              </button>
            </Dialog.Trigger>
            <Dialog.Portal>
              <Dialog.Overlay className="overlay" />
              <Dialog.Content className="mobile-sidebar">
                <Dialog.Title className="sr-only">
                  {text(lang, "Navigation", "导航")}
                </Dialog.Title>
                <Dialog.Description className="sr-only">
                  {text(lang, "Choose a page", "选择页面")}
                </Dialog.Description>
                <Dialog.Close asChild>
                  <button className="close" aria-label="Close">
                    <X size={20} />
                  </button>
                </Dialog.Close>
                {nav}
              </Dialog.Content>
            </Dialog.Portal>
          </Dialog.Root>
          <main className="workspace">
            <div className="breadcrumb">
              {text(lang, "Workspace", "工作区")}
              <span>/</span>
              {labels[lang][page]}
            </div>
            <div className="page-content" key={page}>
              {!can(required[page]) ? (
                <div className="empty">
                  {text(
                    lang,
                    "This capability is not available on this instance.",
                    "此实例尚未提供这项能力。",
                  )}
                </div>
              ) : (
                props &&
                (page === "overview" ? (
                  <Overview {...props} />
                ) : page === "models" ? (
                  <Models
                    {...props}
                    canEdit={can("models.put")}
                    onDirty={setDirty}
                    renderEditor={(id) =>
                      can("models.get") ? (
                        <ModelEditor
                          key={id}
                          {...props}
                          id={id}
                          canEdit={can("models.put")}
                          onDirty={setDirty}
                        />
                      ) : (
                        <p>
                          {text(
                            lang,
                            "Model detail is unavailable.",
                            "模型详情不可用。",
                          )}
                        </p>
                      )
                    }
                  />
                ) : page === "prompt" ? (
                  <PromptEditor
                    {...props}
                    canEdit={can("prompt.put")}
                    onDirty={setDirty}
                  />
                ) : (
                  <Records {...props} />
                ))
              )}
            </div>
            <footer className="workspace-footer">
              Jev Router <span>·</span>{" "}
              {text(lang, "Shared management API", "共享管理 API")}
            </footer>
          </main>
        </>
      )}
    </div>
  );
}
