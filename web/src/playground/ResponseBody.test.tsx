import { it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { ResponseBody } from "./ResponseBody";
it("renders streaming Chinese Markdown and code without active HTML", () => {
  const { container, rerender } = render(
    <ResponseBody content={"中文 **加粗\n\n```js\nconst a = 1;"} streaming />,
  );
  expect(screen.getByText("中文", { exact: false })).toBeInTheDocument();
  expect(container.querySelector("code")).toHaveTextContent("const a = 1");
  rerender(
    <ResponseBody
      content={
        '中文 **加粗**\n\n```js\nconst a = 1;\n```\n\n<img src="https://evil.test/x" onerror="alert(1)"><script>alert(1)</script>\n\n[bad](javascript:alert(1))'
      }
      streaming={false}
    />,
  );
  expect(container.querySelector("script")).toBeNull();
  expect(container.querySelector("[onerror]")).toBeNull();
  expect(container.querySelector('a[href^="javascript:"]')).toBeNull();
  expect(container.querySelector("strong")).toHaveTextContent("加粗");
});
