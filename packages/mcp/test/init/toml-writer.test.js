import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { parse as parseTOML } from "smol-toml";
import { buildMCPServerConfig, writeTOMLMCPConfig } from "../../src/init.js";
import * as tomlOwner from "../../src/init-toml.js";

test("TOML writer serializes concurrent updates and leaves private permissions", async (t) => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-toml-concurrent-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const filePath = path.join(dir, "config.toml");
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "CONCURRENT_CANARY" });
  await Promise.all(["first", "second"].map((name) => writeTOMLMCPConfig(filePath, name, config)));
  const parsed = parseTOML(await fs.readFile(filePath, "utf8"));
  assert.deepEqual(Object.keys(parsed.mcp_servers).sort(), ["first", "second"]);
  for (const server of Object.values(parsed.mcp_servers)) {
    assert.equal(server.env.AIPERMISSION_API_TOKEN, "CONCURRENT_CANARY");
  }
  if (process.platform !== "win32") assert.equal((await fs.stat(filePath)).mode & 0o777, 0o600);
  assert.deepEqual(await fs.readdir(dir), ["config.toml"]);
});

test("TOML preview never renders a supplied bearer token", () => {
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "PREVIEW_CANARY" });
  const preview = tomlOwner.tomlPreviewServerBlock("my.server", config);
  assert.doesNotMatch(preview, /PREVIEW_CANARY/);
  const parsed = parseTOML(preview);
  assert.equal(parsed.mcp_servers["my.server"].env.AIPERMISSION_API_TOKEN, "YOUR_TOKEN_HERE");
  assert.deepEqual(parsed.mcp_servers["my.server"].args, config.args);
});
