import { expect, it } from "vitest";
import { credentialResourcesResponse, gatewayCreatedTokenResponse, gatewayStatusResponse, gatewayTargetsResponse, gatewayTokensResponse, mcpRuntimeResponse } from "./core-resource-contracts.ts";

const target = { target_id: 1, profile_id: 2, project_id: 3, connector_kind: "fixture", ref: "fixture:1:2", target_name: "Target",
  profile_kind: "default", profile_label: "main", project_name: "My Project", project_slug: "my-project", status: "active",
  created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z" };

it("preserves typed target metadata without inventing a runtime", () => {
  expect(gatewayTargetsResponse({ items: [target] })).toEqual([target]);
  const runtime = { ...target, runtime_id: 7, transfer_runtime_id: 8, config: { future: true }, public: { username: "readonly" } };
  expect(gatewayTargetsResponse({ items: [runtime] })).toEqual([runtime]);
});

it.each([null, {}, { items: null }, { items: [{ ...target, profile_id: "2" }] }, { items: [{ ...target, connector_kind: null }] },
  { items: [{ ...target, runtime_id: 0 }] }, { items: [{ ...target, config: [] }] }, { items: [{ ...target, public: "secret" }] },
  { items: [{ ...target, created_at: false }] }, { items: [{ ...target, id: -1 }] }, { items: [{ ...target, name: {} }] }])(
  "rejects malformed targets: %j", (response) => expect(() => gatewayTargetsResponse(response)).toThrow("Invalid gateway targets"),
);

it("validates token identity and lifecycle metadata", () => {
  const token = { id: 1, name: "Client", token: "fixture-token", expires_at: "", revoked_at: "", created_at: "now", updated_at: "now" };
  expect(gatewayTokensResponse([token])).toEqual([token]);
  expect(gatewayTokensResponse([])).toEqual([]);
});

it("requires a show-once token value in creation responses", () => {
  const token = { id: 1, name: "Client", token: "fixture-token" };
  expect(gatewayCreatedTokenResponse(token)).toEqual(token);
  for (const invalid of [{ ...token, token: undefined }, { ...token, token: "" }, { ...token, id: "1" }]) {
    expect(() => gatewayCreatedTokenResponse(invalid)).toThrow("Invalid created token");
  }
});

it.each([null, {}, [{}], [{ id: 1, name: null }], [{ id: 0, name: "Client" }], [{ id: 1, name: "Client", expires_at: {} }]])(
  "rejects malformed tokens: %j", (response) => expect(() => gatewayTokensResponse(response)).toThrow("Invalid gateway tokens"),
);

it("validates gateway health and MCP runtime metadata", () => {
  expect(gatewayStatusResponse({ service: "aipermission", status: "running", config: {}, features: ["fixture"], audit: { queued: 0 } })).toMatchObject({ status: "running", features: ["fixture"] });
  expect(mcpRuntimeResponse({ enabled: true, start_enabled: false, updated_at: "now" })).toEqual({ enabled: true, start_enabled: false, updated_at: "now" });
});

it.each([null, [], {}, { status: "running", service: 1 }, { status: "running", config: [] }, { status: "running", features: [1] }])(
  "rejects malformed health: %j", (response) => expect(() => gatewayStatusResponse(response)).toThrow("Invalid gateway status"),
);

it.each([null, {}, { enabled: true }, { enabled: "true", start_enabled: false }, { enabled: true, start_enabled: false, updated_at: {} }])(
  "rejects malformed MCP state: %j", (response) => expect(() => mcpRuntimeResponse(response)).toThrow("Invalid MCP runtime"),
);

it("keeps connector-owned credential fields while validating common presentation fields", () => {
  const credential = { id: 1, name: "main", resource_kind: "fixture", profile: { public: { username: "readonly" } } };
  expect(credentialResourcesResponse([credential])).toEqual([credential]);
  expect(credentialResourcesResponse([{ name: "named-resource", resource_ref: "fixture:main" }])).toEqual([{ name: "named-resource", resource_ref: "fixture:main" }]);
});

it.each([null, {}, [null], [{ id: false }], [{ id: 0 }], [{ name: {} }], [{ resource_ref: [] }]])(
  "rejects malformed credential resources: %j", (response) => expect(() => credentialResourcesResponse(response)).toThrow("Invalid connector credential"),
);
