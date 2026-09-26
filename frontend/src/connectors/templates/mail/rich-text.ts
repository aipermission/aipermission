const elementNode = 1;
const textNode = 3;
const blockElements = new Set(["blockquote", "div", "h1", "h2", "h3", "h4", "h5", "h6", "p", "pre"]);

export interface RichTextNode {
  nodeType: number;
  nodeValue?: string | null;
  data?: string;
  tagName?: string;
  nodeName?: string;
  childNodes?: ArrayLike<RichTextNode>;
  children?: ArrayLike<RichTextNode>;
  parentElement?: RichTextNode | null;
  parentNode?: RichTextNode | null;
  href?: string;
  getAttribute?: (_name: string) => string | null;
}

export function richTextToPlainText(root: RichTextNode | null | undefined) {
  const chunks: string[] = [];

  function append(value: string) {
    if (value) chunks.push(value);
  }

  function newline() {
    if (chunks.length === 0 || chunks[chunks.length - 1].endsWith("\n")) return;
    chunks.push("\n");
  }

  function visit(node: RichTextNode | null | undefined) {
    if (!node) return;
    if (node.nodeType === textNode) {
      append(node.nodeValue || node.data || "");
      return;
    }
    if (node.nodeType !== elementNode) {
      for (const child of Array.from(node.childNodes || [])) visit(child);
      return;
    }
    visitElement(node);
  }

  function visitElement(node: RichTextNode) {
    const tag = nodeTag(node);
    if (tag === "br") {
      newline();
      return;
    }
    if (blockElements.has(tag) || tag === "li") newline();
    if (tag === "li") append(listMarker(node));
    const anchorStart = chunks.join("").length;
    for (const child of Array.from(node.childNodes || [])) visit(child);
    if (tag === "a") {
      const href = normalizeEditorLink(node.getAttribute?.("href") || node.href || "");
      const anchorText = chunks.join("").slice(anchorStart).trim();
      if (href && !anchorText) append(href);
      else if (href && anchorText !== href) append(` (${href})`);
    }
    if (blockElements.has(tag) || tag === "li") newline();
  }

  for (const child of Array.from(root?.childNodes || [])) visit(child);
  return normalizePlainText(chunks.join(""));
}

function nodeTag(node: RichTextNode) {
  return String(node.tagName || node.nodeName || "").toLowerCase();
}

export function plainTextToHTML(value: unknown) {
  return escapeHTML(String(value || "")).replaceAll("\n", "<br>");
}

export function splitPlainTextLines(value: unknown) {
  return String(value || "")
    .replace(/\r\n?/g, "\n")
    .split("\n");
}

export function normalizeEditorLink(value: unknown) {
  const source = String(value || "").trim();
  if (!source) return "";
  try {
    const url = new URL(source);
    return ["http:", "https:", "mailto:"].includes(url.protocol) ? url.toString() : "";
  } catch {
    return "";
  }
}

function listMarker(node: RichTextNode) {
  const parent = node.parentElement || node.parentNode;
  const parentTag = String(parent?.tagName || parent?.nodeName || "").toLowerCase();
  if (parentTag !== "ol") return "- ";
  const siblings = Array.from(parent?.children || []).filter((item) => String(item.tagName || item.nodeName || "").toLowerCase() === "li");
  return `${Math.max(0, siblings.indexOf(node)) + 1}. `;
}

function normalizePlainText(value: string) {
  const lines = String(value || "")
    .replaceAll("\r", "")
    .split("\n");
  const output: string[] = [];
  let previousBlank = true;
  for (const source of lines) {
    const line = source.replace(/[\t ]+$/g, "");
    const blank = line.trim() === "";
    if (blank && previousBlank) continue;
    output.push(blank ? "" : line);
    previousBlank = blank;
  }
  while (output.at(-1) === "") output.pop();
  return output.join("\n");
}

function escapeHTML(value: string) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}
