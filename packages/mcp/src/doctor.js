import fs from "node:fs/promises";
import { parse as parseTOML } from "smol-toml";
import { parseCommandFlags } from "./cli-flags.js";
import { adaptMCPServerConfig, resolveMCPConfigTarget, resolveSkillTarget } from "./client-registry.js";
import { buildMCPServerConfig, inspectProjectConfigProtection, sanitizeName } from "./init.js";
import { validateSkill } from "./install-skill.js";
import { normalizeLocalAPIURL } from "./local-url.js";
import { assertPrivateFilePermissions, assertTrustedFilePath } from "./private-file.js";

export async function runDoctor(argv = []) {
  const flags = parseCommandFlags("doctor", argv);
  const client = flags.client || flags.provider;
  if (!client) throw new Error("doctor requires --client.");
  const result = await inspectClientSetup({
    client,
    scope: flags.scope,
    mcpScope: flags.mcpScope,
    skillScope: flags.skillScope,
    name: flags.name || "aipermission",
    homeDir: flags.home,
    projectDir: flags.projectDir,
  });
  console.log(`AIPermission MCP doctor: ${result.client} (${result.scope})`);
  for (const entry of result.checks) console.log(`${entry.ok ? "PASS" : "FAIL"} ${entry.label}: ${entry.message}`);
  return result;
}

export async function inspectClientSetup({
  client,
  scope,
  mcpScope,
  skillScope,
  name = "aipermission",
  homeDir,
  projectDir,
  env,
  platform,
  execFile,
}) {
  const serverName = sanitizeName(name);
  const roots = { homeDir, projectDir, env };
  const configTarget = resolveMCPConfigTarget(client, mcpScope || scope, roots);
  const skillTarget = resolveSkillTarget(client, skillScope || scope, roots);
  const checks = [await inspectConfig(configTarget, serverName, { projectDir, platform, execFile }), await inspectSkill(skillTarget)];
  return {
    ok: checks.every((entry) => entry.ok),
    client: configTarget.label,
    scope: configTarget.scope === skillTarget.scope ? configTarget.scope : `MCP ${configTarget.scope}, skill ${skillTarget.scope}`,
    checks,
  };
}

async function inspectConfig(target, name, options) {
  try {
    await assertTrustedFilePath(target.path, { trustedRoot: target.trustedRoot });
    await assertPrivateFilePermissions(target.path, options);
    if (target.projectConfig) {
      await inspectProjectConfigProtection(target.path, options.projectDir || target.trustedRoot);
    }
    const contents = await fs.readFile(target.path, "utf8");
    const server = target.format === "json" ? readJSONServer(contents, target.rootKey, name) : readTOMLServer(contents, name);
    if (!server) return check(false, "MCP config", `server ${name} is missing from ${target.path}`);
    validateServer(target.client, server);
    return check(true, "MCP config", `valid at ${target.path}`);
  } catch (error) {
    if (error.code === "ENOENT") return check(false, "MCP config", `not found at ${target.path}`);
    return check(false, "MCP config", `invalid at ${target.path}: ${safeErrorMessage(error)}`);
  }
}

async function inspectSkill(target) {
  try {
    await assertTrustedFilePath(target.path, { trustedRoot: target.trustedRoot });
    validateSkill(await fs.readFile(target.path, "utf8"));
    return check(true, "Operator skill", `valid at ${target.path}`);
  } catch (error) {
    if (error.code === "ENOENT") return check(false, "Operator skill", `not found at ${target.path}`);
    return check(false, "Operator skill", `invalid at ${target.path}: ${safeErrorMessage(error)}`);
  }
}

function readJSONServer(contents, rootKey, name) {
  let root;
  try {
    root = JSON.parse(contents);
  } catch {
    throw new Error("JSON parsing failed; no file contents were included in this diagnostic");
  }
  const server = root?.[rootKey]?.[name];
  return server && typeof server === "object" && !Array.isArray(server) ? server : null;
}

function readTOMLServer(contents, name) {
  let root;
  try {
    root = parseTOML(contents);
  } catch {
    throw new Error("TOML parsing failed; no file contents were included in this diagnostic");
  }
  const server = root?.mcp_servers?.[name];
  return server && typeof server === "object" && !Array.isArray(server) ? server : null;
}

function validateServer(client, server) {
  const expected = adaptMCPServerConfig(client, buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "TOKEN" }));
  if (server.command !== expected.command) throw new Error("server does not use npx");
  if (!sameArray(server.args, expected.args)) throw new Error(`server does not use the exact ${expected.args[1]} command arguments`);
  if (server.env?.NODE_ENV !== "production") throw new Error("server NODE_ENV is not production");
  if (typeof server.env?.AIPERMISSION_API_TOKEN !== "string" || !server.env.AIPERMISSION_API_TOKEN) {
    throw new Error("server has no API token");
  }
  normalizeLocalAPIURL(server.env?.AIPERMISSION_API_URL);
  for (const [key, value] of Object.entries(expected)) {
    if (["command", "args", "env"].includes(key)) continue;
    if (!sameValue(server[key], value)) throw new Error(`server has an invalid ${key} field for this client`);
  }
}

function sameArray(left, right) {
  return Array.isArray(left) && left.length === right.length && left.every((value, index) => value === right[index]);
}

function sameValue(left, right) {
  return Array.isArray(right) ? sameArray(left, right) : left === right;
}

function safeErrorMessage(error) {
  if (/parsing failed/i.test(error.message || "")) return error.message;
  return String(error.message || error).replace(/[\r\n]+/g, " ");
}

function check(ok, label, message) {
  return { ok, label, message };
}
