import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  analyzeSourceTree,
  dependencyCycles,
  escapedSourceImport,
  hardCodedConnectorKinds,
  moduleGlobSpecifiers,
  moduleSpecifiers,
} from "./architecture-graph.mjs";
import { productionSourceBoundary, productionSourceViolation } from "./production-source-boundary.mjs";

test("collects static imports, re-exports, and literal dynamic imports from the AST", () => {
  const source = `
    import value from "./imported.js";
    export { value as renamed } from "./named.js";
    export * from "./all.js";
    const lazy = import("./lazy.js");
    const template = import(\`./template.js\`);
    const ignored = import(variable);
  `;
  assert.deepEqual(moduleSpecifiers(source), ["./imported.js", "./named.js", "./all.js", "./lazy.js", "./template.js"]);
});

test("rejects executable production modules that escape frontend src", () => {
  const frontendRoot = mkdtempSync(join(tmpdir(), "aipermission-production-source-"));
  try {
    const sourceRoot = join(frontendRoot, "src");
    mkdirSync(join(sourceRoot, "lib"), { recursive: true });
    mkdirSync(join(sourceRoot, "connectors", "templates"), { recursive: true });
    const importer = join(sourceRoot, "lib", "api.js");
    const escaped = join(frontendRoot, "escaped-runtime.js");
    const fakeDependency = join(frontendRoot, "outside", "node_modules", "runtime.js");
    const dependency = join(frontendRoot, "node_modules", "dependency", "index.js");
    writeFileSync(importer, 'import "../../escaped-runtime.js";\n');
    writeFileSync(escaped, `${"export const escaped = true;\n".repeat(600)}`);
    mkdirSync(dirname(fakeDependency), { recursive: true });
    mkdirSync(dirname(dependency), { recursive: true });
    writeFileSync(fakeDependency, "export const fake = true;\n");
    writeFileSync(dependency, "export const dependency = true;\n");

    assert.equal(escapedSourceImport(sourceRoot, importer, "../../escaped-runtime.js"), true);
    assert.equal(escapedSourceImport(sourceRoot, importer, "../inside.js"), false);
    assert.equal(productionSourceViolation(escaped, { frontendRoot, sourceRoot }), escaped);
    assert.equal(productionSourceViolation(fakeDependency, { frontendRoot, sourceRoot }), fakeDependency);
    assert.equal(productionSourceViolation(dependency, { frontendRoot, sourceRoot }), "");
    assert.equal(productionSourceViolation(importer, { frontendRoot, sourceRoot }), "");
    const plugin = productionSourceBoundary({ frontendRoot, sourceRoot });
    assert.throws(
      () => plugin.transform.call({ error: (message) => assert.fail(message) }, "", escaped),
      /Executable production module must live under frontend\/src/,
    );

    const result = analyzeSourceTree(sourceRoot);
    assert.ok(result.failures.some((failure) => failure.includes("imports executable code outside src")));
  } finally {
    rmSync(frontendRoot, { recursive: true, force: true });
  }
});

test("container builds include the production source boundary", () => {
  const dockerfile = readFileSync(new URL("../Dockerfile", import.meta.url), "utf8");
  const copy = "COPY scripts/production-source-boundary.mjs ./scripts/production-source-boundary.mjs";
  const copyIndex = dockerfile.indexOf(copy);
  const buildIndex = dockerfile.indexOf("RUN npm run build");

  assert.ok(copyIndex >= 0, "Dockerfile must copy the production source boundary");
  assert.ok(buildIndex >= 0, "Dockerfile must build the production bundle");
  assert.ok(copyIndex < buildIndex, "the production source boundary must be copied before build");
});

test("collects literal import.meta.glob patterns from the AST", () => {
  const source = `
    const modules = import.meta.glob("./*/index.jsx", { eager: true });
    const metadata = import.meta.glob(["./*/metadata.js", "./*/catalog.js"]);
    const ignored = import.meta.glob(variable);
  `;
  assert.deepEqual(moduleGlobSpecifiers(source), ["./*/index.jsx", "./*/metadata.js", "./*/catalog.js"]);
});

test("finds dependency cycles without duplicating the same cycle", () => {
  const graph = new Map([
    ["a", ["b"]],
    ["b", ["c"]],
    ["c", ["a"]],
  ]);
  assert.deepEqual(dependencyCycles(graph), [["a", "b", "c", "a"]]);
});

test("detects connector literals in branches, switches, and lookup tables", () => {
  const source = `
    const direct = connectorKind === "redis";
    const active = connector.connector_kind;
    const aliased = active === "ssh";
    switch (connectorKind) { case "postgres": break; }
    const connectorLabels = { kafka: "Kafka" };
  `;
  assert.deepEqual(hardCodedConnectorKinds(source, ["kafka", "postgres", "redis", "ssh"]), ["kafka", "postgres", "redis", "ssh"]);
});

test("detects connector literals in array and Set membership checks", () => {
  const source = `
    const connectorKind = target.connector_kind;
    const direct = ["redis", "ssh"].includes(connectorKind);
    const lookup = new Set(["postgres", "kafka"]).has(connectorKind);
  `;
  assert.deepEqual(hardCodedConnectorKinds(source, ["kafka", "postgres", "redis", "ssh"]), ["kafka", "postgres", "redis", "ssh"]);
});

test("rejects production imports through test-support bridges", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-test-bridge-"));
  try {
    for (const directory of ["components", "test", "connectors/templates/fixture"]) {
      mkdirSync(join(root, directory), { recursive: true });
    }
    writeFileSync(join(root, "components/panel.js"), 'export { connector } from "../test/bridge.js";\n');
    writeFileSync(join(root, "test/bridge.js"), 'export { connector } from "../connectors/templates/fixture/model.js";\n');
    writeFileSync(join(root, "connectors/templates/fixture/model.js"), 'export const connector = "fixture";\n');

    const result = analyzeSourceTree(root);
    assert.ok(result.failures.some((failure) => failure.includes("components/panel.js imports test/bridge.js")));
    assert.ok(result.failures.some((failure) => failure.includes("production modules must not import test support")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("covers supported module extensions and rejects unclassified bridge modules", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-extensions-"));
  try {
    for (const directory of ["helpers", "components", "connectors/templates/redis"]) {
      mkdirSync(join(root, directory), { recursive: true });
    }
    writeFileSync(join(root, "App.jsx"), 'import "./helpers/bridge.mjs";\n');
    writeFileSync(join(root, "helpers/bridge.mjs"), 'export * from "../connectors/templates/redis/model.mjs";\n');
    writeFileSync(join(root, "connectors/templates/redis/model.mjs"), "export const model = {};\n");
    writeFileSync(join(root, "components/a.js"), 'import "./b.mjs";\n');
    writeFileSync(join(root, "components/b.mjs"), 'import "./a.js";\n');
    writeFileSync(join(root, "components/oversized.mjs"), "export const line = 1;\nexport const extra = 2;\n");
    writeFileSync(join(root, "components/supported.cjs"), "module.exports = {};\n");
    writeFileSync(join(root, "components/unsupported.cts"), "export const unsupported = true;\n");

    const result = analyzeSourceTree(root, { lineBudget: 1 });
    assert.ok(result.files.some((file) => file.endsWith("helpers/bridge.mjs")));
    assert.ok(result.failures.some((failure) => failure.includes("helpers/bridge.mjs is not in a recognized architecture layer")));
    assert.ok(result.failures.some((failure) => failure.includes("components/oversized.mjs has 2 lines; budget is 1")));
    assert.ok(result.files.some((file) => file.endsWith("components/supported.cjs")));
    assert.ok(result.failures.some((failure) => failure.includes("components/unsupported.cts uses unsupported executable extension .cts")));
    assert.ok(result.failures.some((failure) => failure.includes("dependency cycle:")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("resolves static template imports and rejects unresolved dynamic module loads", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-dynamic-"));
  try {
    for (const directory of ["pages", "connectors/templates/fixture"]) {
      mkdirSync(join(root, directory), { recursive: true });
    }
    writeFileSync(
      join(root, "pages/route.js"),
      'const model = import(`../connectors/templates/fixture/model.mjs`);\nconst unknown = import(modulePath);\nconst modules = import.meta.glob(["./known.js", dynamicPattern]);\n',
    );
    writeFileSync(join(root, "connectors/templates/fixture/model.mjs"), "export const model = {};\n");

    const result = analyzeSourceTree(root);
    assert.ok(result.failures.some((failure) => failure.includes("pages/route.js imports connectors/templates/fixture/model.mjs")));
    assert.ok(result.failures.some((failure) => failure.includes("pages/route.js contains a non-static dynamic import")));
    assert.ok(result.failures.some((failure) => failure.includes("pages/route.js contains a non-static import.meta.glob pattern")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("rejects layer inversions, connector leaks, and source cycles", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-"));
  try {
    for (const directory of ["components", "pages", "connectors/templates/_shared", "connectors/templates/redis"]) {
      mkdirSync(join(root, directory), { recursive: true });
    }
    writeFileSync(join(root, "components/panel.js"), 'import "../pages/route.js"; export const kind = connectorKind === "redis";\n');
    writeFileSync(join(root, "pages/route.js"), 'import "../components/panel.js";\n');
    writeFileSync(join(root, "connectors/templates/_shared/helper.js"), 'export * from "../redis/model.js";\n');
    writeFileSync(join(root, "connectors/templates/redis/model.js"), "export const model = {};\n");

    const result = analyzeSourceTree(root);
    assert.ok(result.failures.some((failure) => failure.includes("components/panel.js imports pages/route.js")));
    assert.ok(result.failures.some((failure) => failure.includes("components/panel.js hard-codes connector kind redis")));
    assert.ok(result.failures.some((failure) => failure.includes("_shared/helper.js imports connectors/templates/redis/model.js")));
    assert.ok(result.failures.some((failure) => failure.includes("dependency cycle:")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("rejects production modules above the line budget", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-lines-"));
  try {
    mkdirSync(join(root, "connectors", "templates", "fixture"), { recursive: true });
    writeFileSync(join(root, "oversized.js"), "export const one = 1;\nexport const two = 2;\n");
    const result = analyzeSourceTree(root, { lineBudget: 1 });
    assert.ok(result.failures.some((failure) => failure.includes("oversized.js has 2 lines; budget is 1")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("classifies only terminal test filename markers as test support", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-test-support-"));
  try {
    mkdirSync(join(root, "lib"), { recursive: true });
    mkdirSync(join(root, "connectors", "templates"), { recursive: true });
    writeFileSync(join(root, "lib", "fixture.test.js"), 'import "./production.js";\n');
    writeFileSync(join(root, "lib", "runtime.test.facade.js"), "export const value = 1;\n");
    writeFileSync(join(root, "lib", "production.js"), 'import "./fixture.test.js";\n');

    const result = analyzeSourceTree(root);
    assert.ok(result.failures.some((failure) => failure.includes("production modules must not import test support")));
    assert.ok(!result.files.some((file) => file.endsWith("fixture.test.js")));
    assert.ok(result.files.some((file) => file.endsWith("runtime.test.facade.js")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("does not classify production modules by test-like directory names", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-test-directory-"));
  try {
    mkdirSync(join(root, "cache.test.fixtures"), { recursive: true });
    mkdirSync(join(root, "connectors", "templates"), { recursive: true });
    writeFileSync(join(root, "cache.test.fixtures", "production.js"), "export const one = 1;\nexport const two = 2;\n");

    const result = analyzeSourceTree(root, { lineBudget: 1 });
    assert.ok(result.files.some((file) => file.endsWith("cache.test.fixtures/production.js")));
    assert.ok(result.failures.some((failure) => failure.includes("cache.test.fixtures/production.js has 2 lines")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("expands glob edges and rejects template imports into registry and page layers", () => {
  const root = mkdtempSync(join(tmpdir(), "aipermission-architecture-glob-"));
  try {
    for (const directory of ["pages", "connectors/templates/fixture"]) {
      mkdirSync(join(root, directory), { recursive: true });
    }
    writeFileSync(join(root, "connectors/templates/registry.jsx"), 'const modules = import.meta.glob("./*/index.jsx");\n');
    writeFileSync(join(root, "connectors/templates/fixture/index.jsx"), 'import "../registry.jsx"; import "../../../pages/route.js";\n');
    writeFileSync(join(root, "pages/route.js"), 'import "../connectors/templates/fixture/index.jsx";\n');

    const result = analyzeSourceTree(root);
    assert.ok(result.failures.some((failure) => failure.includes("dependency cycle:")));
    assert.ok(result.failures.some((failure) => failure.includes("fixture/index.jsx imports connectors/templates/registry.jsx")));
    assert.ok(result.failures.some((failure) => failure.includes("fixture/index.jsx imports pages/route.js")));
    assert.ok(result.failures.some((failure) => failure.includes("pages/route.js imports connectors/templates/fixture/index.jsx")));
    assert.ok(analyzeSourceTree(root, { importBudget: 0 }).failures.some((failure) => failure.includes("registry.jsx imports 1 modules")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
