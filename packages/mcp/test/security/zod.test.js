import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import test from "node:test";
import { z } from "zod";
import { projectGatewaySuccess, responseContracts } from "../../src/response-contracts.js";
import { connectorTargetResponse } from "../support/response-fixtures.js";

test("resolved Zod rejects oversized constrained arrays before validating elements", () => {
  const fromSDK = createRequire(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"));
  const source = `const { z } = require(${JSON.stringify(fromSDK.resolve("zod"))});
    const result = z.array(z.string()).max(8).safeParse(Array(5000).fill(0));
    console.log(JSON.stringify({ success: result.success, issues: result.error?.issues.length }));`;
  const result = spawnSync(process.execPath, ["--max-old-space-size=96", "--eval", source], {
    encoding: "utf8",
    timeout: 5000,
    maxBuffer: 4096,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { success: false, issues: 1 });
});

test("gateway validation stops after the first invalid collection element", () => {
  let visited = 0;
  const schema = z.array(
    z.preprocess((value) => {
      visited++;
      return value;
    }, z.string()),
  );
  assert.throws(() => projectGatewaySuccess(schema, Array(5000).fill(0)), /contract validation/);
  assert.equal(visited, 1);
});

test("connector metadata handles large arrays without a spread-call stack overflow", () => {
  const value = [{ ...connectorTargetResponse, metadata: { labels: Array(200000).fill(0) } }];
  const projected = projectGatewaySuccess(responseContracts.connectorTargets, value);
  assert.equal(projected[0].metadata.labels.length, 200000);
});

test("real Vault response rejects invalid numeric collections within a constrained heap", () => {
  const source = `
    import assert from "node:assert/strict";
    import { projectGatewaySuccess, responseContracts } from ${JSON.stringify(new URL("../../src/response-contracts.js", import.meta.url).href)};
    import { vaultActionResponse } from ${JSON.stringify(new URL("../support/response-fixtures.js", import.meta.url).href)};
    const response = vaultActionResponse({ input: {
      name: "EXAMPLE_KEY", generator_kind: "hex", shared_project_ids: Array(500000).fill(0),
    } });
    assert.ok(Buffer.byteLength(JSON.stringify(response)) < 8 * 1024 * 1024);
    assert.throws(() => projectGatewaySuccess(responseContracts.vaultAction, response),
      { code: "gateway_response_contract_invalid" });
  `;
  const result = spawnSync(process.execPath, ["--max-old-space-size=96", "--input-type=module", "--eval", source], {
    encoding: "utf8",
    timeout: 10000,
    maxBuffer: 4096,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
});
