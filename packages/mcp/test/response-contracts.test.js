import assert from "node:assert/strict";
import test from "node:test";

import { projectGatewaySuccess, responseContracts } from "../src/response-contracts.js";
import { connectorActionResponse, connectorTargetResponse, vaultActionResponse } from "./support/response-fixtures.js";

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

test("Vault contracts reject raw value fields in otherwise valid responses", () => {
  const response = vaultActionResponse({
    input: { name: "PROJECT_TOKEN", generator_kind: "random_token" },
    reason: "Create a scoped token.",
    created_at: "2026-09-17T10:00:00Z",
    expires_at: "2026-09-17T10:15:00Z",
    output: {
      item: {
        vault_ref: "vault:3",
        item_id: 3,
        project_id: 1,
        name: "PROJECT_TOKEN",
        secret_type: "api_key",
        status: "active",
        expires_at: "",
        value_version: 1,
        metadata_revision: 1,
      },
      secret_returned: false,
    },
  });
  assert.doesNotThrow(() => projectGatewaySuccess(responseContracts.vaultAction, response));
  assert.throws(
    () => projectGatewaySuccess(responseContracts.vaultAction, { ...response, output: { ...response.output, value: "must-not-escape" } }),
    /contract validation/,
  );
});

test("Vault action lifecycle requires recorded identity and mutually exclusive output", () => {
  const response = vaultActionResponse();
  assert.doesNotThrow(() => projectGatewaySuccess(responseContracts.vaultAction, response));
  assert.doesNotThrow(() =>
    projectGatewaySuccess(responseContracts.vaultAction, { status: "stopped", error: "Start MCP from the web UI." }),
  );
  for (const invalid of [
    { ...response, status: "invented" },
    { ...response, request_id: undefined },
    { ...response, project_ref: undefined },
    { ...response, action_name: undefined },
    { ...response, secret_values_returned: undefined },
    { ...response, output_withheld: true, output: { item: {} } },
  ]) {
    assert.throws(() => projectGatewaySuccess(responseContracts.vaultAction, invalid), /contract validation/);
  }
});

test("action contracts bind successful responses to the requested identity", () => {
  assert.doesNotThrow(() =>
    projectGatewaySuccess(responseContracts.connectorActionCall, connectorActionResponse, {
      target_ref: connectorActionResponse.target_ref,
      action_name: connectorActionResponse.action_name,
    }),
  );
  assert.throws(
    () => projectGatewaySuccess(responseContracts.connectorActionCall, connectorActionResponse, { target_ref: "redis:2:1" }),
    /contract validation/,
  );
  assert.throws(
    () => projectGatewaySuccess(responseContracts.connectorActionRequest, connectorActionResponse, { request_id: 99 }),
    /contract validation/,
  );

  const vaultResponse = vaultActionResponse();
  assert.doesNotThrow(() =>
    projectGatewaySuccess(responseContracts.vaultAction, vaultResponse, {
      request_id: vaultResponse.request_id,
      project_ref: vaultResponse.project_ref,
      action_name: vaultResponse.action_name,
    }),
  );
  assert.throws(
    () => projectGatewaySuccess(responseContracts.vaultAction, vaultResponse, { request_id: vaultResponse.request_id + 1 }),
    /contract validation/,
  );
  assert.throws(
    () => projectGatewaySuccess(responseContracts.vaultAction, vaultResponse, { request_id: vaultResponse.request_id, status: "canceled" }),
    /contract validation/,
  );
});

test("Vault action contracts keep input and output bound to their action", () => {
  const generated = vaultActionResponse({
    action_name: "generate_item",
    input: { target_ref: "ssh:1:1", items: [{ item_id: 1, source_project_id: 1 }] },
  });
  const restarted = vaultActionResponse({
    action_name: "restart_session_with_environment",
    output: {
      item: {
        vault_ref: "vault:3",
        item_id: 3,
        project_id: 1,
        name: "PROJECT_TOKEN",
        secret_type: "api_key",
        status: "active",
        expires_at: "",
        value_version: 1,
        metadata_revision: 1,
      },
      secret_returned: false,
    },
  });
  assert.throws(() => projectGatewaySuccess(responseContracts.vaultAction, generated), /contract validation/);
  assert.throws(() => projectGatewaySuccess(responseContracts.vaultAction, restarted), /contract validation/);
});
