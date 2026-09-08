import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

import { testReachesOwner } from "./async-owner-manifest.mjs";

test("requires an import-graph relationship instead of an owner comment or filename substring", () => {
  const fixture = mkdtempSync(join(tmpdir(), "aipermission-async-owner-"));
  const sourceRoot = join(fixture, "src");
  mkdirSync(join(sourceRoot, "lib"), { recursive: true });
  const ownerPath = join(sourceRoot, "lib", "use-mail.js");
  const unrelatedPath = join(sourceRoot, "lib", "use-mailbox.js");
  const testPath = join(sourceRoot, "mail.component.test.js");
  writeFileSync(ownerPath, "export const owner = () => setTimeout(loadMessages, 10);\n");
  writeFileSync(unrelatedPath, "export const unrelated = true;\n");
  writeFileSync(testPath, '// async-owner: src/lib/use-mail.js\nimport { unrelated } from "./lib/use-mailbox.js";\nvoid unrelated;\n');
  try {
    const graph = new Map([
      [resolve(ownerPath), []],
      [resolve(unrelatedPath), []],
    ]);
    const options = {
      graph,
      ownerPath: resolve(ownerPath),
      sourceFiles: new Set(graph.keys()),
      sourceRoot,
      testPath,
    };
    assert.equal(testReachesOwner(options), false);

    writeFileSync(testPath, 'import { owner } from "./lib/use-mail.js";\nvoid owner;\n');
    assert.equal(testReachesOwner(options), true);
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
