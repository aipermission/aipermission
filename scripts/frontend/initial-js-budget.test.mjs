import assert from "node:assert/strict";
import { test } from "node:test";
import { initialJavaScriptFiles, requireLazyReleaseNotes, releaseNotesBoundary } from "../../frontend/scripts/initial-js-budget.mjs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";

const require = createRequire(new URL("../../frontend/package.json", import.meta.url));
const { build, normalizePath } = await import(pathToFileURL(require.resolve("vite")).href);

test("loads package-resolved ESM through literal path delimiters", async (context) => {
  const stage = await mkdtemp(join(tmpdir(), "aipermission-notes-loader-"));
  context.after(() => rm(stage, { recursive: true, force: true }));
  for (const name of ["with space", "with#hash", ...(process.platform === "win32" ? [] : ["with?query"])]) {
    const directory = join(stage, name);
    await mkdir(directory);
    await writeFile(join(directory, "module.mjs"), "export default 'loaded';");
    const resolve = createRequire(join(directory, "package.json"));
    const loaded = await import(pathToFileURL(resolve.resolve("./module.mjs")).href);
    assert.equal(loaded.default, "loaded", name);
  }
});

test("measures only eager JavaScript, not dynamically imported terminal chunks", () => {
  const manifest = {
    "index.html": { isEntry: true, file: "assets/index.js", imports: ["_react.js"], dynamicImports: ["terminal.jsx"] },
    "_react.js": { file: "assets/react.js" },
    "terminal.jsx": { file: "assets/terminal.js", imports: ["_react.js"] },
  };
  assert.deepEqual(initialJavaScriptFiles(manifest).sort(), ["assets/index.js", "assets/react.js"]);
  assert.deepEqual(
    initialJavaScriptFiles({
      "index.html": { isEntry: true, file: "entry.mjs", imports: ["shared"] },
      shared: { file: "shared.cjs" },
    }).sort(),
    ["entry.mjs", "shared.cjs"],
  );
});

test("rejects broken eager-import manifest edges", () => {
  assert.throws(
    () => initialJavaScriptFiles({ "index.html": { isEntry: true, file: "assets/index.js", imports: ["missing"] } }),
    /missing/,
  );
});

test("requires a separate on-demand release notes chunk", () => {
  const key = "src/components/changelog-entries.tsx";
  const notes = { isDynamicEntry: true, file: "assets/changelog.js" };
  assert.doesNotThrow(() => requireLazyReleaseNotes({ [key]: notes }, ["assets/index.js"]));
  assert.throws(() => requireLazyReleaseNotes({}, ["assets/index.js"]), /Release notes/);
  assert.throws(() => requireLazyReleaseNotes({ [key]: { ...notes, isDynamicEntry: false } }, []), /Release notes/);
  assert.throws(() => requireLazyReleaseNotes({ [key]: { isDynamicEntry: true } }, []), /Release notes/);
  assert.throws(() => requireLazyReleaseNotes({ [key]: { ...notes, file: "notes.css" } }, []), /Release notes/);
  assert.throws(() => requireLazyReleaseNotes({ [key]: notes }, [notes.file]), /Release notes/);
  assert.doesNotThrow(() => requireLazyReleaseNotes({ [key]: { ...notes, file: "notes.mjs" } }, ["entry.js"]));
});

test("rejects eagerly shared generated payload while permitting shared React and UI", () => {
  const notes = fileURLToPath(new URL("../../frontend/src/lib/release.generated.json", import.meta.url));
  const chunk = (fileName, isEntry, imports, modules) => ({ type: "chunk", fileName, isEntry, imports, modules });
  const bundle = {
    "index.js": chunk("index.js", true, ["shared.js"], {}),
    "shared.js": chunk("shared.js", false, [], { "react.js": {} }),
    "changelog.js": chunk("changelog.js", false, ["shared.js"], { [notes]: {} }),
    "style.css": { type: "asset", fileName: "style.css" },
  };
  const verify = (candidate) =>
    releaseNotesBoundary().generateBundle.call(
      {
        error: (message) => {
          throw new Error(message);
        },
      },
      {},
      candidate,
    );
  assert.doesNotThrow(() => verify(bundle));
  assert.throws(() => verify({ ...bundle, "shared.js": chunk("shared.js", false, [], { [notes]: {} }) }), /deferred chunks/);
  assert.throws(
    () =>
      verify({
        ...bundle,
        "index.js": chunk("index.js", true, ["shared.mjs"], {}),
        "shared.mjs": chunk("shared.mjs", false, [], { [notes]: {} }),
      }),
    /deferred chunks/,
  );
  assert.throws(() => verify({ ...bundle, "unknown.bin": chunk("unknown.bin", false, [], {}) }), /executable chunk/);
  assert.throws(() => verify({ ...bundle, "changelog.js": chunk("changelog.js", false, [], {}) }), /deferred chunks/);
  assert.throws(() => verify({ ...bundle, "index.js": chunk("index.js", true, ["missing.js"], {}) }), /missing/);
  assert.doesNotThrow(() => verify({ ...bundle, "changelog.js": chunk("changelog.js", false, [], { [`${notes}?json`]: {} }) }));
  assert.doesNotThrow(() => verify({ ...bundle, "changelog.js": chunk("changelog.js", false, [], { [`${notes}#json`]: {} }) }));
});

test("qualifies generated payload ownership with actual emitted Vite chunks", async () => {
  const stage = await mkdtemp(join(tmpdir(), "aipermission-notes-boundary-"));
  const notesPath = normalizePath(fileURLToPath(new URL("../../frontend/src/lib/release.generated.json", import.meta.url)));
  const importPath = JSON.stringify(notesPath);
  const deferredImport = 'globalThis.loadNotes = () => import("./notes.js");';
  const config = {
    configFile: false,
    root: stage,
    logLevel: "silent",
    build: { write: false, minify: false, rollupOptions: { input: join(stage, "entry.js") } },
  };
  try {
    await writeFile(join(stage, "notes.js"), `export { default } from ${importPath};`);
    for (const extension of ["js", "mjs"]) {
      const buildConfig = {
        ...config,
        build: {
          ...config.build,
          rollupOptions: {
            ...config.build.rollupOptions,
            output: { entryFileNames: `[name].${extension}`, chunkFileNames: `[name].${extension}` },
          },
        },
        plugins: [releaseNotesBoundary()],
      };
      await writeFile(join(stage, "entry.js"), deferredImport);
      await build(buildConfig);
      await writeFile(join(stage, "entry.js"), `import notes from ${importPath}; globalThis.noteRows = notes.entries; ${deferredImport}`);
      await assert.rejects(build(buildConfig), /deferred chunks/);
    }
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
});
