const elementNode = 1;
const textNode = 3;
const blockElements = new Set(["blockquote", "div", "h1", "h2", "h3", "h4", "h5", "h6", "p", "pre"]);

export function richTextToPlainText(root) {
  const chunks = [];

  function append(value) {
    if (value) chunks.push(value);
  }

  function newline() {
    if (chunks.length === 0 || chunks[chunks.length - 1].endsWith("\n")) return;
    chunks.push("\n");
  }

  function visit(node) {
    if (!node) return;
    if (node.nodeType === textNode) {
      append(node.nodeValue || node.data || "");
      return;
    }
    if (node.nodeType !== elementNode) {
      for (const child of node.childNodes || []) visit(child);
      return;
    }

    const tag = String(node.tagName || node.nodeName || "").toLowerCase();
    if (tag === "br") {
      newline();
      return;
    }
    if (blockElements.has(tag) || tag === "li") newline();
    if (tag === "li") append(listMarker(node));
    const anchorStart = chunks.join("").length;
    for (const child of node.childNodes || []) visit(child);
    if (tag === "a") {
      const href = normalizeEditorLink(node.getAttribute?.("href") || node.href || "");
      const anchorText = chunks.join("").slice(anchorStart).trim();
      if (href && !anchorText) append(href);
      else if (href && anchorText !== href) append(` (${href})`);
    }
    if (blockElements.has(tag) || tag === "li") newline();
  }

  for (const child of root?.childNodes || []) visit(child);
  return normalizePlainText(chunks.join(""));
}

export function plainTextToHTML(value) {
  return escapeHTML(String(value || "")).replaceAll("\n", "<br>");
}

export function splitPlainTextLines(value) {
  return String(value || "").replace(/\r\n?/g, "\n").split("\n");
}

export function normalizeEditorLink(value) {
  const source = String(value || "").trim();
  if (!source) return "";
  try {
    const url = new URL(source);
    return ["http:", "https:", "mailto:"].includes(url.protocol) ? url.toString() : "";
  } catch {
    return "";
  }
}

function listMarker(node) {
  const parent = node.parentElement || node.parentNode;
  const parentTag = String(parent?.tagName || parent?.nodeName || "").toLowerCase();
  if (parentTag !== "ol") return "- ";
  const siblings = Array.from(parent?.children || []).filter((item) => String(item.tagName || item.nodeName || "").toLowerCase() === "li");
  return `${Math.max(0, siblings.indexOf(node)) + 1}. `;
}

function normalizePlainText(value) {
  const lines = String(value || "").replaceAll("\r", "").split("\n");
  const output = [];
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

function escapeHTML(value) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}
