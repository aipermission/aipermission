import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const serverSource = () => fs.readFile(path.resolve("src/server.js"), "utf8");

test("MCP package exposes only connector action tools", async () => {
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
