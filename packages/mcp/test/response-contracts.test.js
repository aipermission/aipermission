import assert from "node:assert/strict";
import test from "node:test";

import { projectGatewaySuccess, responseContracts } from "../src/response-contracts.js";
import { connectorActionResponse, connectorTargetResponse } from "./support/response-fixtures.js";

test("connector action contracts reject unexpected envelope fields without constraining opaque output", () => {
  const projected = projectGatewaySuccess(responseContracts.connectorActionRequest, {
    ...connectorActionResponse,
    output: { password: "domain data", rows: [{ arbitrary_connector_field: true }] },
  });
  assert.deepEqual(projected.output, { password: "domain data", rows: [{ arbitrary_connector_field: true }] });
  assert.throws(() => {
    try {
      projectGatewaySuccess(responseContracts.connectorActionRequest, {
        ...connectorActionResponse,
        output: { arbitrary_connector_field: true },
        provider_secret: "must-not-escape",
      });
    } catch (error) {
      assert.equal(error.code, "gateway_response_contract_invalid");
      throw error;
    }
  }, /contract validation/);
});

test("connector target contracts reject secret-bearing metadata keys", () => {
  assert.doesNotThrow(() =>
    projectGatewaySuccess(responseContracts.connectorTargets, [{ ...connectorTargetResponse, metadata: { host: "127.0.0.1" } }]),
  );
  for (const metadata of [
    { nested: { private_key: "nope" } },
    { nested: { privateKey: "nope" } },
    { "api-token": "nope" },
    { accessKey: "nope" },
  ]) {
    assert.throws(
      () => projectGatewaySuccess(responseContracts.connectorTargets, [{ ...connectorTargetResponse, metadata }]),
      /contract validation/,
    );
  }
});
