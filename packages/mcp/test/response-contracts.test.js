import assert from "node:assert/strict";
import test from "node:test";

import { projectGatewaySuccess, responseContracts } from "../src/response-contracts.js";

test("connector action contracts reject unexpected envelope fields without constraining opaque output", () => {
  const projected = projectGatewaySuccess(responseContracts.connectorAction, {
    status: "completed",
    request_id: 7,
    output: { password: "domain data", rows: [{ arbitrary_connector_field: true }] },
  });
  assert.deepEqual(projected.output, { password: "domain data", rows: [{ arbitrary_connector_field: true }] });
  assert.throws(() => {
    try {
      projectGatewaySuccess(responseContracts.connectorAction, {
        status: "completed",
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
  const target = {
    target_ref: "ssh:1:1",
    project_id: 1,
    project_name: "My Project",
    project_slug: "my-project",
    target_id: 1,
    target_name: "server",
    connector_kind: "ssh",
    profile_id: 1,
    profile_label: "admin",
    profile_kind: "private_key",
    actions: [],
  };
  assert.doesNotThrow(() => projectGatewaySuccess(responseContracts.connectorTargets, [{ ...target, metadata: { host: "127.0.0.1" } }]));
  for (const metadata of [
    { nested: { private_key: "nope" } },
    { nested: { privateKey: "nope" } },
    { "api-token": "nope" },
    { accessKey: "nope" },
  ]) {
    assert.throws(() => projectGatewaySuccess(responseContracts.connectorTargets, [{ ...target, metadata }]), /contract validation/);
  }
});

test("Vault contracts reject raw value fields in otherwise valid responses", () => {
  const response = {
    status: "completed",
    request_id: 9,
    project_ref: "my-project",
    action_name: "generate_item",
    input: { name: "PROJECT_TOKEN", generator_kind: "random_token" },
    reason: "Create a scoped token.",
    created_at: "2026-09-17T10:00:00Z",
    expires_at: "2026-09-17T10:15:00Z",
    secret_values_returned: false,
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
  };
  assert.doesNotThrow(() => projectGatewaySuccess(responseContracts.vaultAction, response));
  assert.throws(
    () => projectGatewaySuccess(responseContracts.vaultAction, { ...response, output: { ...response.output, value: "must-not-escape" } }),
    /contract validation/,
  );
});
