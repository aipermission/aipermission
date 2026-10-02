import { applyEdits, getNodeValue, modify, parseTree } from "jsonc-parser";

export function parseJSONCConfig(source) {
  const errors = [];
  const tree = parseTree(source, errors, { allowTrailingComma: true });
  if (errors.length || tree?.type !== "object") {
    throw new Error("Could not parse VS Code JSONC config.");
  }
  return getNodeValue(tree);
}

export function updateJSONCServer(content, rootKey, name, config) {
  const source = content.trim() ? content : "{}\n";
  let root;
  try {
    root = parseJSONCConfig(source);
  } catch {
    throw new Error("Could not parse VS Code JSONC config; the existing file was left unchanged.");
  }
  const servers = root[rootKey];
  if (servers !== undefined && (!servers || typeof servers !== "object" || Array.isArray(servers))) {
    throw new Error("VS Code MCP servers must be an object; the existing file was left unchanged.");
  }
  const eol = source.includes("\r\n") ? "\r\n" : "\n";
  let next;
  try {
    next = applyEdits(
      source,
      modify(source, [rootKey, name], config, {
        getInsertionIndex: () => 0,
        formattingOptions: { insertSpaces: true, tabSize: 2, eol },
      }),
    );
  } catch {
    throw new Error("Could not safely update VS Code JSONC config; the existing file was left unchanged.");
  }
  let updated;
  try {
    updated = parseJSONCConfig(next);
  } catch {
    throw new Error("Could not safely update VS Code JSONC config; the existing file was left unchanged.");
  }
  if (JSON.stringify(updated?.[rootKey]?.[name]) !== JSON.stringify(config)) {
    throw new Error("Could not safely update VS Code JSONC config; the existing file was left unchanged.");
  }
  return next;
}
