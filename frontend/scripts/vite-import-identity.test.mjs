import assert from "node:assert/strict";
import test from "node:test";
import { build, version } from "vite";
import { data, createSourceFixture } from "./import-source-fixtures.mjs";

import productionImports from "./production-test-import-check.cjs";
import importIdentity from "./vite-import-identity.cjs";

const { analyzeProductionTestImports } = productionImports;
const { executableImportIdentity } = importIdentity;
const policy = data.frontendPolicy;

async function viteTraversesSource(frontendRoot, owner) {
  const result = await build({
    ...data.viteBuild,
    root: frontendRoot,
    build: {
      ...data.viteBuild.build,
      rollupOptions: { input: owner },
    },
  });
  const outputs = (Array.isArray(result) ? result : [result]).flatMap((bundle) => bundle.output);
  // Raw source/URL assets contain the import text, but not this dependency's side effect.
  return outputs.some((output) => String(output.type === "chunk" ? output.code : output.source).includes("__viteContractExecuted"));
}

for (const [suffix, executes] of data.viteCases) {
  test(`matches offline Vite ${version} source traversal: ${suffix}`, async (context) => {
    const { root, frontendRoot, owner, helper, result } = createSourceFixture(context, "vite", {
      source: `import value from ${JSON.stringify("./helper.test.js" + suffix)}; globalThis.imported = value;`,
    });
    const specifier = `./helper.test.js${suffix}`;
    assert.equal(await viteTraversesSource(frontendRoot, owner), executes, "actual Vite traversal");
    assert.equal(executableImportIdentity(specifier), executes ? "./helper.test.js" : null, "identity must match Vite");
    assert.deepEqual(result.graph.get(owner), executes ? [helper] : []);
    assert.equal(
      result.failures.some((failure) => failure.includes("production modules must not import test support")),
      executes,
    );
    assert.deepEqual(
      analyzeProductionTestImports(root, policy),
      executes ? ["frontend/src/lib/owner.js imports test support frontend/src/lib/helper.test.js"] : [],
    );
  });
}

test(`offline Vite ${version} traverses worker-url bridge to raw-hash test support`, async (context) => {
  const { root, frontendRoot, owner, helper, bridge, result } = createSourceFixture(context, "vite-bridge");
  assert.equal(await viteTraversesSource(frontendRoot, owner), true, "worker bundle must traverse helper source");
  assert.deepEqual(result.graph.get(owner), [bridge]);
  assert.deepEqual(result.graph.get(bridge), [helper]);
  assert.ok(result.failures.some((failure) => failure.includes("production modules must not import test support")));
  assert.deepEqual(analyzeProductionTestImports(root, policy), [
    "frontend/src/lib/bridge.js imports test support frontend/src/lib/helper.test.js",
  ]);
});
