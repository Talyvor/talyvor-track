import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { MarkdownPreview } from "./Templates";

afterEach(cleanup);

describe("template preview comments (CodeQL js/bad-tag-filter, js/incomplete-multi-character-sanitization)", () => {
  it("shows a comment line's words as text and never as markup", () => {
    const { container } = render(
      <MarkdownPreview
        source={[
          "<!-- <img src=x onerror=alert(1)> -->",
          "<!-- a --!> <script>alert(2)</script> -->",
          "<!--<!-- <iframe src=x> -->-->",
        ].join("\n")}
      />,
    );
    expect(container.querySelector("img, script, iframe")).toBeNull();
    const rows = Array.from(container.querySelectorAll("div.italic")).map((d) => d.textContent);
    expect(rows).toEqual([
      "<img src=x onerror=alert(1)>",
      "a --!> <script>alert(2)</script>",
      "<!-- <iframe src=x> -->",
    ]);
  });
});
