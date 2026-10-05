import assert from "node:assert/strict";
import { createRequire } from "node:module";
import {
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";
import { monacoSanitizer } from "../../frontend/scripts/monaco-sanitizer.mjs";

const require = createRequire(
  new URL("../../frontend/package.json", import.meta.url),
);
const consumer = require
  .resolve("monaco-editor/base/browser/domSanitize")
  .replaceAll("\\", "/");
const vendored = require
  .resolve("monaco-editor/base/browser/dompurify/dompurify")
  .replaceAll("\\", "/");
const replacement = resolve(
  require.resolve("dompurify"),
  "../purify.es.mjs",
).replaceAll("\\", "/");
const { optimizeDeps, resolveConfig } = await import(
  new URL(
    "../../frontend/node_modules/vite/dist/node/index.js",
    import.meta.url,
  )
);

test("only Monaco's exact sanitizer consumer resolves to the pinned package", () => {
  const plugin = monacoSanitizer();
  assert.equal(
    plugin.resolveId("./dompurify/dompurify.js", consumer),
    replacement,
  );
  assert.equal(
    plugin.resolveId("./dompurify/dompurify.js", `${consumer}?import`),
    replacement,
  );
  for (const [source, importer] of [
    ["./other.js", consumer],
    ["./dompurify/dompurify.js", "/other/domSanitize.js"],
    ["./dompurify/dompurify.js", undefined],
  ])
    assert.equal(plugin.resolveId(source, importer), null);
});

test("literal path delimiters preserve exact module identity before URL suffixes", async () => {
  const directory = await mkdtemp(join(tmpdir(), "aipermission-monaco-path-"));
  try {
    for (const name of [
      "path#fragment",
      ...(process.platform === "win32" ? [] : ["path?query"]),
    ]) {
      const root = join(directory, name);
      const modules = join(root, "node_modules");
      const monaco = join(modules, "monaco-editor");
      const purify = join(modules, "dompurify");
      await mkdir(monaco, { recursive: true });
      await mkdir(purify);
      await writeFile(
        join(monaco, "package.json"),
        JSON.stringify({
          exports: {
            "./base/browser/domSanitize": "./consumer.js",
            "./base/browser/dompurify/dompurify": "./vendored.js",
          },
        }),
      );
      await writeFile(
        join(purify, "package.json"),
        JSON.stringify({ exports: "./replacement.mjs" }),
      );
      for (const file of [
        join(monaco, "consumer.js"),
        join(monaco, "vendored.js"),
        join(purify, "replacement.mjs"),
      ])
        await writeFile(file, "export default {};");
      await symlink(
        fileURLToPath(
          new URL("../../frontend/node_modules/vite", import.meta.url),
        ),
        join(modules, "vite"),
        "junction",
      );
      const script = join(root, "bridge.mjs");
      await copyFile(
        new URL("../../frontend/scripts/monaco-sanitizer.mjs", import.meta.url),
        script,
      );
      const plugin = (
        await import(pathToFileURL(script).href)
      ).monacoSanitizer();
      const normalize = (file) => file.replaceAll("\\", "/");
      const exactConsumer = normalize(join(monaco, "consumer.js"));
      const exactReplacement = normalize(join(purify, "replacement.mjs"));
      for (const suffix of ["", "?import", "#fragment"])
        assert.equal(
          plugin.resolveId(
            "./dompurify/dompurify.js",
            `${exactConsumer}${suffix}`,
          ),
          exactReplacement,
        );
      assert.equal(
        plugin.resolveId("./dompurify/dompurify.js", `${exactConsumer}.other`),
        null,
      );
      const check = (ids) =>
        plugin.generateBundle.call({
          getModuleIds: () => ids.values(),
          error: (message) => {
            throw new Error(message);
          },
        });
      check([exactConsumer, exactReplacement]);
      assert.throws(() => check([exactConsumer]), /pinned DOMPurify/);
      assert.throws(
        () => check([normalize(join(monaco, "vendored.js"))]),
        /pinned DOMPurify/,
      );
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("production graph rejects embedded or missing sanitizer replacement", () => {
  const plugin = monacoSanitizer();
  const check = (ids) =>
    plugin.generateBundle.call({
      getModuleIds: () => ids.values(),
      error: (message) => {
        throw new Error(message);
      },
    });
  check([]);
  check([consumer, replacement]);
  assert.throws(() => check([consumer]), /pinned DOMPurify/);
  assert.throws(
    () => check([consumer, replacement, vendored]),
    /pinned DOMPurify/,
  );
  assert.throws(() => check([`${vendored}?import`]), /pinned DOMPurify/);
});

test("Vite development optimization uses the patched sanitizer, not Monaco's embedded copy", async () => {
  const root = await mkdtemp(join(tmpdir(), "aipermission-monaco-optimizer-"));
  try {
    const config = await resolveConfig(
      {
        configFile: false,
        root,
        cacheDir: join(root, "cache"),
        logLevel: "silent",
        plugins: [monacoSanitizer()],
        optimizeDeps: { include: [consumer], noDiscovery: true },
      },
      "serve",
    );
    const metadata = await optimizeDeps(config, true);
    const entry = metadata.optimized[consumer];
    assert.ok(entry, "Monaco sanitizer consumer must actually be optimized");
    const output = await readFile(entry.file, "utf8");
    assert.match(output, /3\.4\.16/);
    assert.doesNotMatch(output, /3\.4\.15/);
    const map = JSON.parse(await readFile(`${entry.file}.map`, "utf8"));
    assert.ok(
      map.sources.some((source) =>
        source.endsWith("node_modules/dompurify/src/purify.ts"),
      ),
    );
    assert.ok(
      !map.sources.some((source) =>
        source.endsWith("browser/dompurify/dompurify.js"),
      ),
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
