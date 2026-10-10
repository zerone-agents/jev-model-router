import { Streamdown, type Components } from "streamdown";
import "streamdown/styles.css";
const components: Components = {
  strong: ({ children }) => <strong>{children}</strong>,
  em: ({ children }) => <em>{children}</em>,
  del: ({ children }) => <del>{children}</del>,
  code: ({ children }) => <code>{children}</code>,
  pre: ({ children }) => <pre>{children}</pre>,
  table: ({ children }) => (
    <div className="pg-table">
      <table>{children}</table>
    </div>
  ),
  a: ({ href, children }) => (
    <a
      href={href && /^(https?:\/\/|mailto:)/i.test(href) ? href : undefined}
      target="_blank"
      rel="noopener noreferrer"
    >
      {children}
    </a>
  ),
  // Images are not part of the text-only playground. Never fetch generated URLs.
  img: ({ alt }) => <span className="muted">[{alt || "image"}]</span>,
};
export function ResponseBody({
  content,
  streaming,
}: {
  content: string;
  streaming: boolean;
}) {
  return (
    <Streamdown
      className="pg-markdown"
      components={components}
      controls={false}
      skipHtml
      rehypePlugins={[]}
      isAnimating={streaming}
      animated={{
        animation: "fadeIn",
        duration: 120,
        stagger: 0,
        maxBacklogMs: 0,
      }}
    >
      {content}
    </Streamdown>
  );
}
