const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

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

const policy = {
  frontendArchitecture: { testModuleMarkers: [".test.", ".spec."] },
  sourceBudgets: [
    { directory: "src", extensions: [".js"], classifier: "markers" },
    { directory: "test", extensions: [".js"], classifier: "all" },
  ],
};

function withImportFixture(name, verify) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), `aipermission-import-${name}-`));
  try {
    fs.mkdirSync(path.join(root, "src"));
    fs.mkdirSync(path.join(root, "test"));
    verify(root);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}

test("rejects production imports of terminal test modules across governed roots", () => {
  withImportFixture("owner", (root) => {
    fs.writeFileSync(path.join(root, "src", "owner.js"), 'export { helper } from "../test/helper.test.js";\n');
    fs.writeFileSync(path.join(root, "test", "helper.test.js"), "export const helper = true;\n");
    assert.deepEqual(analyzeProductionTestImports(root, policy), ["src/owner.js imports test support test/helper.test.js"]);
  });
});

test("does not grant test status to a production facade filename", () => {
  withImportFixture("facade", (root) => {
    fs.writeFileSync(path.join(root, "src", "owner.js"), 'import "./runtime.test.facade.js";\n');
    fs.writeFileSync(path.join(root, "src", "runtime.test.facade.js"), "export const helper = true;\n");
    assert.deepEqual(analyzeProductionTestImports(root, policy), []);
  });
});

test("rejects test imports expressed as static template literals", () => {
  withImportFixture("template", (root) => {
    fs.writeFileSync(
      path.join(root, "src", "owner.js"),
      "const imported = import(`../test/helper.test.js`);\nconst required = require(`../test/other.test.js`);\n",
    );
    fs.writeFileSync(path.join(root, "test", "helper.test.js"), "export const helper = true;\n");
    fs.writeFileSync(path.join(root, "test", "other.test.js"), "export const other = true;\n");
    assert.deepEqual(analyzeProductionTestImports(root, policy), [
      "src/owner.js imports test support test/helper.test.js",
      "src/owner.js imports test support test/other.test.js",
    ]);
  });
});

test("fails closed when a dynamic module specifier cannot be classified", () => {
  withImportFixture("dynamic", (root) => {
    fs.writeFileSync(path.join(root, "src", "owner.js"), "const modulePath = './unknown.js';\nimport(modulePath);\n");
    assert.deepEqual(analyzeProductionTestImports(root, policy), [
      "src/owner.js cannot be parsed for import ownership: dynamic import specifier cannot be verified",
    ]);
  });
});

test("ignores dependency trees inside governed tooling roots", () => {
  withImportFixture("dependencies", (root) => {
    fs.mkdirSync(path.join(root, "src", "node_modules", "dependency"), { recursive: true });
    fs.writeFileSync(path.join(root, "src", "owner.js"), "export const owner = true;\n");
    fs.writeFileSync(path.join(root, "src", "node_modules", "dependency", "index.js"), "import(dynamic);\n");
    assert.deepEqual(analyzeProductionTestImports(root, policy), []);
  });
});
