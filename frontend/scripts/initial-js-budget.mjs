import { readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

export const initialJavaScriptFiles = (manifest) => {
  const visited = new Set();
  const files = new Set();
  function visit(key) {
    if (visited.has(key)) return;
    visited.add(key);
    const entry = manifest[key];
    if (!entry) throw new Error(`Build manifest is missing eager import ${key}`);
    if (entry.file?.endsWith(".js")) files.add(entry.file);
    for (const imported of entry.imports || []) visit(imported);
  }
  for (const [key, entry] of Object.entries(manifest)) {
    if (entry.isEntry && entry.file?.endsWith(".js")) visit(key);
  }
  if (files.size === 0) throw new Error("Build manifest has no initial JavaScript entry");
  return [...files];
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const dist = join(fileURLToPath(new URL("..", import.meta.url)), "dist");
  const manifest = JSON.parse(await readFile(join(dist, ".vite/manifest.json"), "utf8"));
  const files = initialJavaScriptFiles(manifest);
  if (files.some((file) => /(?:^|\/)terminal-[^/]+\.js$/.test(file))) {
    throw new Error("Terminal JavaScript must be loaded only when opening a terminal route or dialog");
  }
  const sizes = await Promise.all(files.map(async (file) => (await stat(join(dist, file))).size));
  const bytes = sizes.reduce((sum, size) => sum + size, 0);
  const budget = 1300 * 1024;
  process.stdout.write(`Initial JavaScript: ${bytes} bytes in ${files.length} eager files (budget ${budget}).\n`);
  if (bytes > budget) throw new Error("Initial JavaScript exceeds the measured startup budget");
}
