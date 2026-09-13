import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { behaviorOwnerManifestWeakening, validateBehaviorOwnerManifest } from "./behavior-owner-manifest.mjs";

test("protected behavior owners cannot be removed from the ratcheted manifest", () => {
  const failures = behaviorOwnerManifestWeakening(
    { version: 1, owners: { "src/notice.jsx": ["src/notice.test.jsx"] } },
    { version: 1, owners: {} },
  );
  assert.deepEqual(failures, ["protected behavior owner was removed: src/notice.jsx"]);
});

test("protected behavior test mappings cannot be removed from the ratcheted manifest", () => {
  const failures = behaviorOwnerManifestWeakening(
    { version: 1, owners: { "src/notice.jsx": ["src/notice.component.test.jsx"] } },
    { version: 1, owners: { "src/notice.jsx": ["src/replacement.component.test.jsx"] } },
  );
  assert.deepEqual(failures, ["protected behavior test mapping was removed: src/notice.jsx -> src/notice.component.test.jsx"]);
});

test("protected behavior tests must exist and reach their owner", (t) => {
  const frontendRoot = mkdtempSync(join(tmpdir(), "aipermission-owner-"));
  t.after(() => rmSync(frontendRoot, { recursive: true, force: true }));
  const sourceRoot = join(frontendRoot, "src");
  mkdirSync(sourceRoot, { recursive: true });
  writeFileSync(join(sourceRoot, "notice.jsx"), "export const Notice = () => null;\n");
  writeFileSync(join(sourceRoot, "unrelated.component.test.jsx"), 'it("runs", () => {});\n');
  const sourceFiles = new Set([join(sourceRoot, "notice.jsx"), join(sourceRoot, "unrelated.component.test.jsx")]);
  const graph = new Map([[join(sourceRoot, "unrelated.component.test.jsx"), []]]);
  const failures = validateBehaviorOwnerManifest(
    {
      version: 1,
      owners: {
        "src/notice.jsx": ["src/missing.component.test.jsx", "src/unrelated.component.test.jsx"],
      },
    },
    { frontendRoot, graph, sourceFiles },
  );
  assert.deepEqual(failures, [
    "protected behavior test is missing: src/missing.component.test.jsx",
    "protected behavior tests do not reach their owner: src/notice.jsx",
  ]);

  writeFileSync(join(sourceRoot, "replacement.component.test.jsx"), 'import "./notice.jsx";\nit("covers notice", () => {});\n');
  sourceFiles.add(join(sourceRoot, "replacement.component.test.jsx"));
  graph.set(join(sourceRoot, "replacement.component.test.jsx"), [join(sourceRoot, "notice.jsx")]);
  assert.deepEqual(
    validateBehaviorOwnerManifest(
      { version: 1, owners: { "src/notice.jsx": ["src/replacement.component.test.jsx"] } },
      { frontendRoot, graph, sourceFiles },
    ),
    [],
  );
});

test("protected behavior tests must run in a configured suite and declare a test", (t) => {
  const frontendRoot = mkdtempSync(join(tmpdir(), "aipermission-owner-suite-"));
  t.after(() => rmSync(frontendRoot, { recursive: true, force: true }));
  const sourceRoot = join(frontendRoot, "src");
  mkdirSync(sourceRoot, { recursive: true });
  writeFileSync(join(sourceRoot, "notice.jsx"), "export const notice = true;\n");
  writeFileSync(join(sourceRoot, "import-only.component.test.jsx"), 'import "./notice.jsx";\n// it("does not run", () => {});\n');
  writeFileSync(join(sourceRoot, "unconfigured.spec.jsx"), 'import "./notice.jsx";\ntest("notice", () => {});\n');
  const sourceFiles = new Set([
    join(sourceRoot, "notice.jsx"),
    join(sourceRoot, "import-only.component.test.jsx"),
    join(sourceRoot, "unconfigured.spec.jsx"),
  ]);
  const graph = new Map([
    [join(sourceRoot, "import-only.component.test.jsx"), [join(sourceRoot, "notice.jsx")]],
    [join(sourceRoot, "unconfigured.spec.jsx"), [join(sourceRoot, "notice.jsx")]],
  ]);
  assert.deepEqual(
    validateBehaviorOwnerManifest(
      {
        version: 1,
        owners: {
          "src/notice.jsx": ["src/import-only.component.test.jsx", "src/unconfigured.spec.jsx"],
        },
      },
      { frontendRoot, graph, sourceFiles },
    ),
    [
      "protected behavior test has no executable test declaration: src/import-only.component.test.jsx",
      "protected behavior test is outside the configured test suites: src/unconfigured.spec.jsx",
    ],
  );
});
