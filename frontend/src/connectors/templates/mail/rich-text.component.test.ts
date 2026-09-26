import { describe, expect, it } from "vitest";
import { normalizeEditorLink, plainTextToHTML, richTextToPlainText } from "./rich-text";

describe("Mail rich text DOM projection", () => {
  it("keeps real DOM list order, line breaks, and safe link text", () => {
    const root = document.createElement("div");
    root.innerHTML =
      '<p>Hello <a href="https://example.com/docs">docs</a></p><ol><li>First</li><li>Second<br>line</li></ol><ul><li>Other</li></ul>';
    expect(richTextToPlainText(root)).toBe("Hello docs (https://example.com/docs)\n1. First\n2. Second\nline\n- Other");
  });

  it("does not promote unsafe URLs into the plain-text fallback", () => {
    const root = document.createElement("div");
    root.innerHTML = '<a href="javascript:alert(1)">Visible label</a><p><a href="https://example.com/empty"></a></p>';
    expect(richTextToPlainText(root)).toBe("Visible label\nhttps://example.com/empty");
    expect(normalizeEditorLink("data:text/html,unsafe")).toBe("");
    expect(normalizeEditorLink("mailto:operator@example.com")).toBe("mailto:operator@example.com");
    expect(plainTextToHTML("<script>'quoted' & \"double\"\nnext")).toBe("&lt;script&gt;&#39;quoted&#39; &amp; &quot;double&quot;<br>next");
  });

  it("normalizes fragment whitespace without dropping intentional paragraph breaks", () => {
    const fragment = document.createDocumentFragment();
    const element = document.createElement("div");
    element.innerHTML = "<p>one  </p><br><br><p>two\r\nthree</p>";
    fragment.append(element);
    expect(richTextToPlainText(fragment)).toBe("one\ntwo\nthree");
    expect(richTextToPlainText(null)).toBe("");
  });
});
