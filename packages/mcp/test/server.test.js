import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { callVaultActionSchema, listVaultItemsSchema, vaultActionRequestSchema } from "../src/vault-tools.js";

const serverSource = () => fs.readFile(path.resolve("src/server.js"), "utf8");

test("MCP package does not expose retired pre-connector tools", async () => {
	const source = await serverSource();

	assert.doesNotMatch(source, /server\.tool\(\s*"list_servers"/);
	assert.doesNotMatch(source, /server\.tool\(\s*"exec"/);
	assert.doesNotMatch(source, /server\.tool\(\s*"read_console"/);
	assert.doesNotMatch(source, /server\.tool\(\s*"get_request"/);
	assert.doesNotMatch(source, /server\.tool\(\s*"start_file_download"/);
	assert.doesNotMatch(source, /server\.tool\(\s*"upload_files"/);
	assert.doesNotMatch(source, /\/api\/mcp\/exec/);
	assert.doesNotMatch(source, /\/api\/mcp\/servers/);
});

test("Vault tools route through secret-free MCP Vault APIs", async () => {
  const source = await serverSource();

  assert.match(source, /server\.tool\(\s*"list_vault_items"/);
  assert.match(source, /server\.tool\(\s*"call_vault_action"/);
  assert.match(source, /server\.tool\(\s*"get_vault_action_request"/);
  assert.match(source, /server\.tool\(\s*"cancel_vault_action_request"/);
  assert.match(source, /apiGet\(`\/api\/mcp\/vault-items/);
  assert.match(source, /apiPost\("\/api\/mcp\/vault-actions\/call"/);
  assert.match(source, /apiGet\(`\/api\/mcp\/vault-action-requests\/\$\{request_id\}`\)/);
  assert.match(source, /apiPost\(`\/api\/mcp\/vault-action-requests\/\$\{request_id\}\/cancel`/);
});

test("Vault tool schemas enforce the public MCP contract", () => {
  const parse = (schema, value) => Object.fromEntries(
    Object.entries(schema).map(([key, field]) => [key, field.parse(value[key])]),
  );
  assert.deepEqual(parse(listVaultItemsSchema, {}), { project_ref: undefined });
  assert.throws(() => parse(callVaultActionSchema, {
    project_ref: "my-project",
    action_name: "generate_item",
    input: {},
    reason: "",
    idempotency_key: "request-1",
  }));
  assert.throws(() => parse(callVaultActionSchema, {
    project_ref: "my-project",
    action_name: "reveal_secret",
    input: {},
    reason: "Not allowed.",
    idempotency_key: "request-1",
  }));
  assert.throws(() => parse(callVaultActionSchema, {
    project_ref: "my-project",
    action_name: "generate_item",
    input: {},
    reason: "Generate approved metadata.",
    idempotency_key: "x".repeat(129),
  }));
  assert.equal(vaultActionRequestSchema.request_id.parse(42), 42);
  assert.throws(() => vaultActionRequestSchema.request_id.parse(0));
});

test("connector tools route through the MCP connector API", async () => {
  const source = await serverSource();

  assert.match(source, /server\.tool\(\s*"list_connector_targets"/);
  assert.match(source, /server\.tool\(\s*"get_connector_help"/);
  assert.match(source, /server\.tool\(\s*"get_connector_actions"/);
  assert.match(source, /server\.tool\(\s*"call_connector_action"/);
  assert.match(source, /server\.tool\(\s*"get_connector_action_request"/);
  assert.match(source, /apiGet\("\/api\/mcp\/connector-targets"/);
  assert.match(source, /apiGet\(`\/api\/mcp\/connector-help\?\$\{params\.toString\(\)\}`\)/);
  assert.match(source, /apiPost\("\/api\/mcp\/connector-actions\/call"/);
  assert.match(source, /apiGet\(`\/api\/mcp\/connector-action-requests\/\$\{request_id\}`\)/);
});
