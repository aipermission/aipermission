import { readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const isJavaScriptFile = (file) => typeof file === "string" && /\.(?:js|mjs|cjs)$/.test(file);

export const initialJavaScriptFiles = (manifest) => {
  const visited = new Set();
  const files = new Set();
  function visit(key) {
    if (visited.has(key)) return;
    visited.add(key);
    const entry = manifest[key];
    if (!entry) throw new Error(`Build manifest is missing eager import ${key}`);
    if (isJavaScriptFile(entry.file)) files.add(entry.file);
    for (const imported of entry.imports || []) visit(imported);
  }
  for (const [key, entry] of Object.entries(manifest)) {
    if (entry.isEntry && isJavaScriptFile(entry.file)) visit(key);
  }
  if (files.size === 0) throw new Error("Build manifest has no initial JavaScript entry");
  return [...files];
};

export function requireLazyReleaseNotes(manifest, initialFiles) {
  const notes = manifest["src/components/changelog-entries.tsx"];
  if (!notes?.isDynamicEntry || !isJavaScriptFile(notes.file) || initialFiles.includes(notes.file)) {
    throw new Error("Release notes must be loaded only when opening the changelog");
  }
}

export function releaseNotesBoundary() {
  const notesPath = fileURLToPath(new URL("../src/lib/release.generated.json", import.meta.url)).replaceAll("\\", "/");
  return {
    name: "aipermission-lazy-release-notes",
    generateBundle(_options, bundle) {
      const chunks = Object.values(bundle).filter((entry) => entry.type === "chunk");
      if (chunks.some((chunk) => !isJavaScriptFile(chunk.fileName))) this.error("Unsupported executable chunk filename");
      const manifest = Object.fromEntries(
        chunks.map((chunk) => [chunk.fileName, { file: chunk.fileName, isEntry: chunk.isEntry, imports: chunk.imports }]),
      );
      const startup = new Set(initialJavaScriptFiles(manifest));
      const owners = chunks.filter((chunk) =>
        Object.keys(chunk.modules).some((id) => {
          const normalized = id.replaceAll("\\", "/");
          return normalized === notesPath || normalized.startsWith(`${notesPath}?`) || normalized.startsWith(`${notesPath}#`);
        }),
      );
      if (owners.length === 0 || owners.some((chunk) => startup.has(chunk.fileName))) {
        this.error("Generated release notes must belong only to deferred chunks");
      }
    },
  };
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const dist = join(fileURLToPath(new URL("..", import.meta.url)), "dist");
  const manifest = JSON.parse(await readFile(join(dist, ".vite/manifest.json"), "utf8"));
  const files = initialJavaScriptFiles(manifest);
  requireLazyReleaseNotes(manifest, files);
  if (files.some((file) => /(?:^|\/)terminal-[^/]+\.(?:js|mjs|cjs)$/.test(file))) {
    throw new Error("Terminal JavaScript must be loaded only when opening a terminal route or dialog");
  }
  const sizes = await Promise.all(files.map(async (file) => (await stat(join(dist, file))).size));
  const bytes = sizes.reduce((sum, size) => sum + size, 0);
  const budget = 1300 * 1024;
  process.stdout.write(`Initial JavaScript: ${bytes} bytes in ${files.length} eager files (budget ${budget}).\n`);
  if (bytes > budget) throw new Error("Initial JavaScript exceeds the measured startup budget");
}
