import { readFile } from "node:fs/promises";

export async function readActionExamples(file) {
  const markdown = await readFile(new URL(`../../../../${file}`, import.meta.url), "utf8");
  const examples = [];
  for (const match of markdown.matchAll(/^```json\r?\n([\s\S]*?)^```\s*$/gm)) {
    // Other REST fences include JSON message streams, not single MCP envelopes.
    if (!/"action_name"\s*:/.test(match[1]) || !/"(?:target_ref|project_ref)"\s*:/.test(match[1])) continue;
    const value = JSON.parse(match[1]);
    if (!value.action_name || !(value.target_ref || value.project_ref)) continue;
    examples.push({ file, line: markdown.slice(0, match.index).split("\n").length, value });
  }
  return examples;
}
