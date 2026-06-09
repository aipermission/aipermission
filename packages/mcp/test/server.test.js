import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const serverSource = () => fs.readFile(path.resolve("src/server.js"), "utf8");

test("MCP command tools keep multi-server execution on exec", async () => {
  const source = await serverSource();

  assert.match(source, /server\.tool\(\s*"exec"/);
  assert.doesNotMatch(source, /server\.tool\(\s*"bulk_exec"/);
  assert.match(source, /server_ids: z\.array\(z\.number\(\)\.int\(\)\.positive\(\)\)\.min\(1\)\.max\(25\)\.optional\(\)/);
  assert.match(source, /apiPost\("\/api\/mcp\/bulk-exec"/);
});

test("read_console accepts either one server id or multiple server ids", async () => {
  const source = await serverSource();

  assert.match(source, /server\.tool\(\s*"read_console"/);
  assert.match(source, /Use either server_id or server_ids, not both/);
  assert.match(source, /server_ids must not contain duplicates/);
});
