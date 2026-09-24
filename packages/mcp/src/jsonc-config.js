import { applyEdits, modify, parse } from "jsonc-parser";

export function updateJSONCServer(content, rootKey, name, config) {
  const source = content.trim() ? content : "{}\n";
  const errors = [];
  const root = parse(source, errors, { allowTrailingComma: true });
  if (errors.length || !root || typeof root !== "object" || Array.isArray(root)) {
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
  const nextErrors = [];
  const updated = parse(next, nextErrors, { allowTrailingComma: true });
  if (nextErrors.length || JSON.stringify(updated?.[rootKey]?.[name]) !== JSON.stringify(config)) {
    throw new Error("Could not safely update VS Code JSONC config; the existing file was left unchanged.");
  }
  return next;
}
