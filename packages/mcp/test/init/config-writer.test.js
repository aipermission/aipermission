import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildMCPServerConfig, writeJSONMCPConfig, writeProviderConfig, writeTOMLMCPConfig } from "../../src/init.js";
import * as init from "../../src/init.js";
import * as configOwner from "../../src/init-config.js";
import * as gitOwner from "../../src/init-git.js";
import * as tomlOwner from "../../src/init-toml.js";
import { privateLockPath } from "../../src/private-file.js";
import { parseJSONCConfig } from "../../src/jsonc-config.js";

test("init preserves its public exports and exposes the implementation owners directly", () => {
  assert.deepEqual(
    Object.keys(init).sort(),
    [
      "PACKAGE_NAME",
      "PACKAGE_SPECIFIER",
      "PACKAGE_VERSION",
      "assertProviderSelectionAvailable",
      "buildMCPServerConfig",
      "inspectProjectConfigProtection",
      "normalizeURL",
      "parseFlags",
      "runInit",
      "runSetup",
      "sanitizeName",
      "tomlKey",
      "tomlString",
      "writeJSONMCPConfig",
      "writeProviderConfig",
      "writeTOMLMCPConfig",
    ].sort(),
  );
  assert.equal(init.writeProviderConfig, configOwner.writeProviderConfig);
  assert.equal(init.writeJSONMCPConfig, configOwner.writeJSONMCPConfig);
  assert.equal(init.inspectProjectConfigProtection, gitOwner.inspectProjectConfigProtection);
  assert.equal(init.writeTOMLMCPConfig, tomlOwner.writeTOMLMCPConfig);
  assert.equal(init.tomlKey, tomlOwner.tomlKey);
  assert.equal(init.tomlString, tomlOwner.tomlString);
});

for (const format of ["JSON", "JSONC", "TOML"]) {
  test(`${format} writer runs both callbacks under lock and preserves the file when the second rejects`, async (t) => {
    const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-init-callback-"));
    t.after(() => fs.rm(dir, { recursive: true, force: true }));
    const filePath = path.join(dir, "config");
    const original = format === "TOML" ? 'title = "keep"\n' : '{ "note": "keep" }\n';
    await fs.writeFile(filePath, original, { mode: 0o600 });
    let calls = 0;
    const options = {
      trustedRoot: dir,
      jsonc: format === "JSONC",
      beforeWrite: async () => {
        calls += 1;
        assert.equal((await fs.stat(privateLockPath(filePath))).isFile(), true);
        assert.equal(await fs.readFile(filePath, "utf8"), original);
        if (calls === 2) throw new Error("injected pre-write rejection");
      },
    };
    const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "CALLBACK_CANARY" });
    const write = () =>
      format === "TOML"
        ? writeTOMLMCPConfig(filePath, "aipermission", config, options)
        : writeJSONMCPConfig(filePath, "aipermission", config, "servers", options);
    await assert.rejects(write, /injected pre-write rejection/);
    assert.equal(calls, 2);
    assert.equal(await fs.readFile(filePath, "utf8"), original);
    assert.deepEqual(await fs.readdir(dir), ["config"]);
  });

  test(`${format} writer rejects a link-managed config without changing its target`, async (t) => {
    const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-init-link-"));
    t.after(() => fs.rm(dir, { recursive: true, force: true }));
    const filePath = path.join(dir, "config");
    const managedPath = path.join(dir, "managed");
    const original = format === "TOML" ? 'title = "keep"\n' : "{}\n";
    await fs.writeFile(managedPath, original);
    await fs.symlink("managed", filePath);
    const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "LINK_CANARY" });
    const write = () =>
      format === "TOML"
        ? writeTOMLMCPConfig(filePath, "aipermission", config, { trustedRoot: dir })
        : writeJSONMCPConfig(filePath, "aipermission", config, "servers", { trustedRoot: dir, jsonc: format === "JSONC" });
    await assert.rejects(write, /symbolic-link config/);
    assert.equal(await fs.readFile(managedPath, "utf8"), original);
    assert.equal((await fs.lstat(filePath)).isSymbolicLink(), true);
  });
}

test("VS Code writer preserves own prototype-named JSONC records through the safe parser", async (t) => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-init-jsonc-records-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const filePath = path.join(dir, ".vscode", "mcp.json");
  await fs.mkdir(path.dirname(filePath));
  await fs.writeFile(filePath, '// keep\n{ "servers": { "__proto__": { "note": "keep" }, "constructor": { "note": "also keep" } }, }\n');
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "JSONC_RECORD_CANARY" });
  await writeProviderConfig("vscode", "aipermission", config, { projectDir: dir });
  const content = await fs.readFile(filePath, "utf8");
  const parsed = parseJSONCConfig(content);
  assert.equal(Object.getPrototypeOf(parsed.servers), null);
  assert.equal(parsed.servers.__proto__.note, "keep");
  assert.equal(parsed.servers.constructor.note, "also keep");
  assert.equal(parsed.servers.aipermission.env.AIPERMISSION_API_TOKEN, "JSONC_RECORD_CANARY");
  assert.deepEqual(JSON.parse(JSON.stringify(parsed.servers.aipermission)), config);
  assert.match(content, /\/\/ keep/);
  assert.equal(Object.prototype.note, undefined);
});
