const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  analyzeProductionTestImports,
} = require("./production-test-import-check");

const policy = {
  frontendArchitecture: { testModuleMarkers: [".test.", ".spec."] },
  sourceBudgets: [
    { directory: "src", extensions: [".js"], classifier: "markers" },
    { directory: "test", extensions: [".js"], classifier: "all" },
  ],
};

test("rejects production imports of terminal test modules across governed roots", () => {
  const root = fs.mkdtempSync(
    path.join(os.tmpdir(), "aipermission-import-owner-"),
  );
  try {
    fs.mkdirSync(path.join(root, "src"));
    fs.mkdirSync(path.join(root, "test"));
    fs.writeFileSync(
      path.join(root, "src", "owner.js"),
      'export { helper } from "../test/helper.test.js";\n',
    );
    fs.writeFileSync(
      path.join(root, "test", "helper.test.js"),
      "export const helper = true;\n",
    );
    assert.deepEqual(analyzeProductionTestImports(root, policy), [
      "src/owner.js imports test support test/helper.test.js",
    ]);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("does not grant test status to a production facade filename", () => {
  const root = fs.mkdtempSync(
    path.join(os.tmpdir(), "aipermission-import-facade-"),
  );
  try {
    fs.mkdirSync(path.join(root, "src"));
    fs.mkdirSync(path.join(root, "test"));
    fs.writeFileSync(
      path.join(root, "src", "owner.js"),
      'import "./runtime.test.facade.js";\n',
    );
    fs.writeFileSync(
      path.join(root, "src", "runtime.test.facade.js"),
      "export const helper = true;\n",
    );
    assert.deepEqual(analyzeProductionTestImports(root, policy), []);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
