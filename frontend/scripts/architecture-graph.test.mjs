import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { data, createFixture, createSourceFixture } from "./import-source-fixtures.mjs";

import {
  analyzeSourceTree,
  dependencyCycles,
  escapedSourceImport,
  hardCodedConnectorKinds,
  moduleGlobSpecifiers,
  moduleSpecifiers,
  parseModule,
} from "./architecture-graph.mjs";

for (const suffix of data.executableSuffixes) {
  test(`resolves decorated module owners and unsafe test edges: ${suffix}`, (context) => {
    const { root, result } = createSourceFixture(context, "identity", { suffix });
    assert.deepEqual(result.graph.get(join(root, "lib/owner.js")), [join(root, "lib/dependency.js"), join(root, "test/helper.js")]);
    assert.deepEqual(result.failures, [
      "lib/owner.js imports test/helper.js across forbidden boundary: production modules must not import test support",
    ]);
  });
}

test("decorated imports retain extensionless and directory-index resolution and cycles", (context) => {
  const { root, result } = createSourceFixture(context, "candidates");
  assert.deepEqual(result.graph.get(join(root, "lib/owner.js")), [join(root, "lib/dependency.js"), join(root, "lib/helpers/index.js")]);
  assert.ok(result.failures.some((failure) => failure.startsWith("dependency cycle:")));
});

for (const suffix of data.dataSuffixes) {
  test(`data-only imports create no executable dependency or escape: ${suffix}`, (context) => {
    const { root, result } = createSourceFixture(context, "data", { suffix });
    assert.deepEqual(result.graph.get(join(root, "lib/owner.js")), []);
    assert.deepEqual(result.failures, []);
  });
}

for (const suffix of data.escapeSuffixes) {
  test(`decorated executable imports still fail the source escape boundary: ${suffix}`, (context) => {
    const specifier = `../../outside.js${suffix}`;
    const { root } = createFixture(context, "escape", { specifier: JSON.stringify(specifier) });
    assert.deepEqual(analyzeSourceTree(root).failures, [`lib/owner.js imports executable code outside src: ${specifier}`]);
  });
}

test("retains the transitive worker bridge edge into raw-hash test support", (context) => {
  const { root, result } = createSourceFixture(context, "worker-bridge");
  assert.deepEqual(result.graph.get(join(root, "lib/owner.js")), [join(root, "lib/bridge.js")]);
  assert.deepEqual(result.graph.get(join(root, "lib/bridge.js")), [join(root, "lib/helper.test.js")]);
  assert.deepEqual(result.failures, [
    "lib/bridge.js imports lib/helper.test.js across forbidden boundary: production modules must not import test support",
  ]);
});

test("keeps import edges when typed security contracts are analyzed", () => {
  const parsed = parseModule(
    'import { model } from "./security-contracts";\ntype Model = { id: number };\nexport const result: Model = { id: model };',
    "owner.ts",
  );
  assert.deepEqual(moduleSpecifiers(parsed), ["./security-contracts"]);
});

test("excludes declaration-only files from the runtime architecture graph", (context) => {
  const { result } = createSourceFixture(context, "declarations");
  assert.deepEqual(result.failures, []);
  assert.equal(
    result.files.some((file) => file.endsWith("runtime.d.ts")),
    false,
  );
});
import { productionSourceBoundary, productionSourceViolation } from "./production-source-boundary.mjs";

test("collects static imports, re-exports, and literal dynamic imports from the AST", () => {
  const source = data.sources.moduleSpecifiers;
  assert.deepEqual(moduleSpecifiers(source), ["./imported.js", "./named.js", "./all.js", "./lazy.js", "./template.js"]);
});

test("rejects executable production modules that escape frontend src", (context) => {
  const {
    root: frontendRoot,
    sourceRoot,
    importer,
    escaped,
    fakeDependency,
    dependency,
    result,
  } = createSourceFixture(context, "source-escape");
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
  assert.ok(result.failures.some((failure) => failure.includes("imports executable code outside src")));
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
  const source = data.sources.globSpecifiers;
  assert.deepEqual(moduleGlobSpecifiers(source), ["./*/index.ts", "./*/metadata.js", "./*/catalog.js"]);
});

test("finds dependency cycles without duplicating the same cycle", () => {
  const graph = new Map(data.cycleGraph);
  assert.deepEqual(dependencyCycles(graph), [["a", "b", "c", "a"]]);
});

test("detects connector literals in branches, switches, and lookup tables", () => {
  const source = data.sources.connectorBranches;
  assert.deepEqual(hardCodedConnectorKinds(source, ["kafka", "postgres", "redis", "ssh"]), ["kafka", "postgres", "redis", "ssh"]);
});

test("detects connector literals in array and Set membership checks", () => {
  const source = data.sources.connectorMembership;
  assert.deepEqual(hardCodedConnectorKinds(source, ["kafka", "postgres", "redis", "ssh"]), ["kafka", "postgres", "redis", "ssh"]);
});

test("rejects production imports through test-support bridges", (context) => {
  const { result } = createSourceFixture(context, "test-bridge");
  assert.ok(result.failures.some((failure) => failure.includes("components/panel.js imports test/bridge.js")));
  assert.ok(result.failures.some((failure) => failure.includes("production modules must not import test support")));
});

test("covers supported module extensions and rejects unclassified bridge modules", (context) => {
  const { result } = createSourceFixture(context, "extensions");
  assert.ok(result.files.some((file) => file.endsWith("helpers/bridge.mjs")));
  assert.ok(result.failures.some((failure) => failure.includes("helpers/bridge.mjs is not in a recognized architecture layer")));
  assert.ok(result.failures.some((failure) => failure.includes("components/oversized.mjs has 2 lines; budget is 1")));
  assert.ok(result.files.some((file) => file.endsWith("components/supported.cjs")));
  assert.ok(result.failures.some((failure) => failure.includes("components/unsupported.cts uses unsupported executable extension .cts")));
  assert.ok(result.failures.some((failure) => failure.includes("dependency cycle:")));
});

test("resolves static template imports and rejects unresolved dynamic module loads", (context) => {
  const { result } = createSourceFixture(context, "dynamic");
  assert.ok(result.failures.some((failure) => failure.includes("pages/route.js imports connectors/templates/fixture/model.mjs")));
  assert.ok(result.failures.some((failure) => failure.includes("pages/route.js contains a non-static dynamic import")));
  assert.ok(result.failures.some((failure) => failure.includes("pages/route.js contains a non-static import.meta.glob pattern")));
});

test("rejects layer inversions, connector leaks, and source cycles", (context) => {
  const { result } = createSourceFixture(context, "layers");
  assert.ok(result.failures.some((failure) => failure.includes("components/panel.js imports pages/route.js")));
  assert.ok(result.failures.some((failure) => failure.includes("components/panel.js hard-codes connector kind redis")));
  assert.ok(result.failures.some((failure) => failure.includes("_shared/helper.js imports connectors/templates/redis/model.js")));
  assert.ok(result.failures.some((failure) => failure.includes("dependency cycle:")));
});

test("rejects production modules above the line budget", (context) => {
  const { result } = createSourceFixture(context, "lines");
  assert.ok(result.failures.some((failure) => failure.includes("oversized.js has 2 lines; budget is 1")));
});

test("classifies only terminal test filename markers as test support", (context) => {
  const { result } = createSourceFixture(context, "test-support");
  assert.ok(result.failures.some((failure) => failure.includes("production modules must not import test support")));
  assert.ok(!result.files.some((file) => file.endsWith("fixture.test.js")));
  assert.ok(result.files.some((file) => file.endsWith("runtime.test.facade.js")));
});

test("does not classify production modules by test-like directory names", (context) => {
  const { result } = createSourceFixture(context, "test-directory");
  assert.ok(result.files.some((file) => file.endsWith("cache.test.fixtures/production.js")));
  assert.ok(result.failures.some((failure) => failure.includes("cache.test.fixtures/production.js has 2 lines")));
});

test("expands glob edges and rejects template imports into registry and page layers", (context) => {
  const { root, result } = createSourceFixture(context, "glob");
  assert.ok(result.failures.some((failure) => failure.includes("dependency cycle:")));
  assert.ok(result.failures.some((failure) => failure.includes("fixture/index.ts imports connectors/templates/registry.tsx")));
  assert.ok(result.failures.some((failure) => failure.includes("fixture/index.ts imports pages/route.ts")));
  assert.ok(result.failures.some((failure) => failure.includes("pages/route.ts imports connectors/templates/fixture/index.ts")));
  assert.ok(analyzeSourceTree(root, { importBudget: 0 }).failures.some((failure) => failure.includes("registry.tsx imports 1 modules")));
});

test("native family registrations may not import their captured registry", (context) => {
  for (const registry of ["credential-registry", "connector-family-registry", "console-model-registry", "console-recovery-registry"]) {
    const { result } = createSourceFixture(context, "registry", { registry });
    assert.ok(result.failures.some((failure) => failure.includes(`fixture/index.ts imports connectors/templates/${registry}.ts`)));
    assert.ok(!result.failures.some((failure) => failure.includes(`${registry}.ts imports connectors/templates/fixture/index.ts across`)));
  }
});
