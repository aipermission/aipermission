import assert from "node:assert/strict";
import test from "node:test";
import { projectGatewaySuccess, responseContracts } from "../../src/response-contracts.js";
import { connectorActionMaximumInputBytes } from "../../src/generated-connector-contract.js";

const action = {
  name: "inspect",
  label: "Inspect",
  description: "Inspect state",
  risk: "read",
  input_schema: { fields: [] },
  retry_policy: { class: "read_only", guidance: "Read again." },
  max_input_bytes: 1024,
};

test("action catalogs preserve bounded published input limits", () => {
  for (const max_input_bytes of [1, 1024, connectorActionMaximumInputBytes]) {
    const response = { items: [{ ...action, max_input_bytes }] };
    assert.deepEqual(projectGatewaySuccess(responseContracts.connectorActions, response), response);
  }
});

test("action catalogs reject missing or out-of-contract input limits", () => {
  for (const max_input_bytes of [undefined, 0, -1, 1.5, "1024", null, NaN, Infinity, connectorActionMaximumInputBytes + 1]) {
    assert.throws(
      () => projectGatewaySuccess(responseContracts.connectorActions, { items: [{ ...action, max_input_bytes }] }),
      /contract validation/,
    );
  }
});
