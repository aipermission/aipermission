import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { analyzeSourceTree, dependencyCycles, hardCodedConnectorKinds, moduleSpecifiers } from "./architecture-graph.mjs";

test("collects static imports, re-exports, and literal dynamic imports from the AST", () => {
  const source = `
    import value from "./imported.js";
    export { value as renamed } from "./named.js";
    export * from "./all.js";
    const lazy = import("./lazy.js");
    const ignored = import(variable);
  `;
  assert.deepEqual(moduleSpecifiers(source), ["./imported.js", "./named.js", "./all.js", "./lazy.js"]);
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
    switch (connectorKind) { case "postgres": break; }
    const connectorLabels = { kafka: "Kafka" };
  `;
  assert.deepEqual(hardCodedConnectorKinds(source, ["kafka", "postgres", "redis"]), ["kafka", "postgres", "redis"]);
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
