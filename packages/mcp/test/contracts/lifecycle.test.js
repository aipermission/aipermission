import assert from "node:assert/strict";
import test from "node:test";

import { projectGatewaySuccess, responseContracts } from "../../src/response-contracts.js";

const actionResponse = {
  status: "completed",
  request_id: 7,
  target_ref: "redis:1:1",
  connector_kind: "redis",
  action_name: "get_string",
  retry_policy: { class: "read_only", guidance: "Read again if needed." },
};

test("connector action lifecycle requires known statuses, retry classes, and recorded request identity", () => {
  assert.doesNotThrow(() => projectGatewaySuccess(responseContracts.connectorActionCall, actionResponse));
  for (const invalid of [
    { ...actionResponse, status: "invented" },
    { ...actionResponse, status: "running", request_id: undefined },
    { ...actionResponse, retry_policy: { class: "invented", guidance: "retry" } },
  ]) {
    assert.throws(() => projectGatewaySuccess(responseContracts.connectorActionCall, invalid), /contract validation/);
  }
  assert.doesNotThrow(() => projectGatewaySuccess(responseContracts.connectorActionCall, { status: "stopped", error: "Start MCP." }));
  assert.throws(
    () => projectGatewaySuccess(responseContracts.connectorActionRequest, { status: "stopped", error: "Start MCP." }),
    /contract validation/,
  );
  assert.throws(
    () => projectGatewaySuccess(responseContracts.connectorActionRequest, { ...actionResponse, output_withheld: true, output: "leak" }),
    /contract validation/,
  );
  assert.doesNotThrow(() =>
    projectGatewaySuccess(responseContracts.connectorActionRequest, {
      ...actionResponse,
      target_ref: "",
      connector_kind: "",
      output_withheld: true,
    }),
  );
  assert.throws(
    () => projectGatewaySuccess(responseContracts.connectorActionRequest, { ...actionResponse, target_ref: "" }),
    /contract validation/,
  );
});
