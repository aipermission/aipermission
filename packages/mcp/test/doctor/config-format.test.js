import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { inspectClientSetup } from "../../src/doctor.js";
import { buildMCPServerConfig, writeProviderConfig } from "../../src/init.js";
import { installSkill } from "../../src/install-skill.js";

async function setupDoctor(t, client = "vscode", token = "SECRET") {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-"));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const scope = client === "vscode" ? "project" : "user";
  const options = { client, scope, [scope === "project" ? "projectDir" : "homeDir"]: directory };
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token });
  const written = await writeProviderConfig(client, "aipermission", config, options);
  await installSkill(options);
  return { options, config, written };
}

test("doctor reads VS Code JSONC comments and trailing commas without changing the file", async (t) => {
  const { options, config, written } = await setupDoctor(t, "vscode", "JSONC_CANARY_SECRET");
  const source = `{// preserved comment\n"servers":{"aipermission":${JSON.stringify({ ...config, type: "stdio" })},},}\n`;
  await fs.writeFile(written.path, source, { mode: 0o600 });

  const result = await inspectClientSetup(options);

  assert.equal(result.ok, true);
  assert.equal(await fs.readFile(written.path, "utf8"), source);
  assert.doesNotMatch(JSON.stringify(result), /CANARY|SECRET/);
});

for (const client of ["cursor", "copilot"]) {
  test(`doctor keeps ${client} configs strict JSON`, async (t) => {
    const { options, written } = await setupDoctor(t, client, "STRICT_CANARY_SECRET");
    const source = await fs.readFile(written.path, "utf8");
    await fs.writeFile(written.path, source.replace("{", "{// not valid JSON\n"), { mode: 0o600 });

    const result = await inspectClientSetup(options);

    assert.equal(result.ok, false);
    assert.match(result.checks[0].message, /JSON parsing failed/);
    assert.doesNotMatch(JSON.stringify(result), /CANARY|SECRET/);
  });
}

for (const source of ['{"servers":{"aipermission":JSONC_CANARY_SECRET}}', '{"servers":{}, /* JSONC_CANARY_SECRET']) {
  test("doctor rejects malformed VS Code JSONC without parser context or file changes", async (t) => {
    const { options, written } = await setupDoctor(t);
    await fs.writeFile(written.path, source, { mode: 0o600 });

    const result = await inspectClientSetup(options);

    assert.equal(result.ok, false);
    assert.match(result.checks[0].message, /JSON parsing failed/);
    assert.doesNotMatch(JSON.stringify(result), /CANARY|SECRET/);
    assert.equal(await fs.readFile(written.path, "utf8"), source);
  });
}

for (const inherited of ["servers", "server fields"]) {
  test(`doctor does not accept inherited VS Code ${inherited}`, async (t) => {
    const { options, config: baseConfig, written } = await setupDoctor(t, "vscode", "PROTOTYPE_CANARY_SECRET");
    const config = { ...baseConfig, type: "stdio" };
    const source =
      inherited === "servers"
        ? `{"__proto__":{"servers":{"aipermission":${JSON.stringify(config)}}}}`
        : `{"servers":{"aipermission":{"__proto__":${JSON.stringify(config)}}}}`;
    await fs.writeFile(written.path, source, { mode: 0o600 });

    const result = await inspectClientSetup(options);

    assert.equal(result.ok, false);
    assert.doesNotMatch(JSON.stringify(result), /CANARY|SECRET/);
    assert.equal(await fs.readFile(written.path, "utf8"), source);
  });
}
