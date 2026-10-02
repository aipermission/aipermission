const assert = require("node:assert/strict");
const test = require("node:test");
const { data, createFixture } = require("./import-fixtures.cjs");

const { analyzeProductionTestImports, staticModuleSpecifiers } = require("./production-test-import-check.cjs");

test("tracks runtime imports through typed frontend modules", () => {
  assert.deepEqual(
    staticModuleSpecifiers(
      'type Item = { id: number };\nimport { helper } from "./helper.test.js";\nexport const item: Item = { id: helper };',
      "owner.ts",
    ),
    ["./helper.test.js"],
  );
});

test("ignores declaration-only modules with no runtime ownership", () => {
  assert.deepEqual(staticModuleSpecifiers('import type { Fixture } from "./helper.test.js";', "owner.d.ts"), []);
});

const policy = data.importPolicy;

for (const suffix of data.executableSuffixes) {
  for (const statement of [
    (specifier) => `import ${JSON.stringify(specifier)};`,
    (specifier) => `export * from ${JSON.stringify(specifier)};`,
    (specifier) => `import(\`${specifier}\`);`,
  ]) {
    const source = statement(`../test/helper.test.js${suffix}`);
    test(`rejects decorated test import: ${source}`, (context) => {
      const { root } = createFixture(context, "test-import", { source });
      assert.deepEqual(analyzeProductionTestImports(root, policy), ["src/owner.js imports test support test/helper.test.js"]);
    });
  }
}

for (const specifier of ["../test/helper.test?v=1", "../test/helpers?worker"]) {
  test(`resolves decorated extensionless and index test imports: ${specifier}`, (context) => {
    const { root } = createFixture(context, "import-candidates", { specifier: JSON.stringify(specifier) });
    const dependency = specifier.includes("helpers") ? "test/helpers/index.js" : "test/helper.test.js";
    assert.deepEqual(analyzeProductionTestImports(root, policy), [`src/owner.js imports test support ${dependency}`]);
  });
}

test("resolves Vite source-root imports to test owners", (context) => {
  const { root } = createFixture(context, "import-root");
  const frontendPolicy = data.frontendPolicy;
  assert.deepEqual(analyzeProductionTestImports(root, frontendPolicy), [
    "frontend/src/owner.js imports test support frontend/src/helper.test.js",
  ]);
});

for (const suffix of data.dataSuffixes) {
  test(`does not execute test source imported as data: ${suffix}`, (context) => {
    const { root } = createFixture(context, "test-import", { source: `import ${JSON.stringify(`../test/helper.test.js${suffix}`)};` });
    assert.deepEqual(analyzeProductionTestImports(root, policy), []);
  });
}

test("rejects the transitive worker bridge import of raw-hash test support", (context) => {
  const { root } = createFixture(context, "import-worker-bridge");
  assert.deepEqual(analyzeProductionTestImports(root, policy), ["src/bridge.js imports test support test/helper.test.js"]);
});

test("rejects production imports of terminal test modules across governed roots", (context) => {
  const { root } = createFixture(context, "test-import", { source: 'export { helper } from "../test/helper.test.js";\n' });
  assert.deepEqual(analyzeProductionTestImports(root, policy), ["src/owner.js imports test support test/helper.test.js"]);
});

test("does not grant test status to a production facade filename", (context) => {
  const { root } = createFixture(context, "facade");
  assert.deepEqual(analyzeProductionTestImports(root, policy), []);
});

test("rejects test imports expressed as static template literals", (context) => {
  const { root } = createFixture(context, "template");
  assert.deepEqual(analyzeProductionTestImports(root, policy), [
    "src/owner.js imports test support test/helper.test.js",
    "src/owner.js imports test support test/other.test.js",
  ]);
});

test("fails closed when a dynamic module specifier cannot be classified", (context) => {
  const { root } = createFixture(context, "import-dynamic");
  assert.deepEqual(analyzeProductionTestImports(root, policy), [
    "src/owner.js cannot be parsed for import ownership: dynamic import specifier cannot be verified",
  ]);
});

test("ignores dependency trees inside governed tooling roots", (context) => {
  const { root } = createFixture(context, "dependencies");
  assert.deepEqual(analyzeProductionTestImports(root, policy), []);
});
