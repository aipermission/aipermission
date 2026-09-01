import fs from "node:fs/promises";
import os from "node:os";
import { resolveMCPConfigTarget, resolveSkillTarget } from "./client-registry.js";
import { PACKAGE_SPECIFIER, parseFlags, sanitizeName, tomlKey } from "./init.js";
import { normalizeLocalAPIURL } from "./local-url.js";

export async function runDoctor(argv = []) {
  const flags = parseFlags(argv);
  const client = flags.client || flags.provider;
  if (!client) {
    throw new Error("doctor requires --client.");
  }
  const result = await inspectClientSetup({
    client,
    scope: flags.scope,
    name: flags.name || "aipermission",
    homeDir: flags.home || os.homedir(),
    projectDir: flags.projectDir || process.cwd(),
  });
  console.log(`AIPermission MCP doctor: ${result.client} (${result.scope})`);
  for (const check of result.checks) {
    console.log(`${check.ok ? "PASS" : "FAIL"} ${check.label}: ${check.message}`);
  }
  return result;
}

export async function inspectClientSetup({ client, scope, name = "aipermission", homeDir = os.homedir(), projectDir = process.cwd() }) {
  const serverName = sanitizeName(name);
  const configTarget = resolveMCPConfigTarget(client, scope, { homeDir, projectDir });
  const skillTarget = resolveSkillTarget(client, scope, { homeDir, projectDir });
  const checks = [await inspectConfig(configTarget, serverName), await inspectSkill(skillTarget)];
  return {
    ok: checks.every((check) => check.ok),
    client: configTarget.label,
    scope: configTarget.scope,
    checks,
  };
}

async function inspectConfig(target, name) {
  let contents;
  let stat;
  try {
    [contents, stat] = await Promise.all([fs.readFile(target.path, "utf8"), fs.stat(target.path)]);
  } catch (error) {
    if (error.code === "ENOENT") return check(false, "MCP config", `not found at ${target.path}`);
    return check(false, "MCP config", `could not read ${target.path}: ${error.message}`);
  }
  try {
    if (process.platform !== "win32" && (stat.mode & 0o077) !== 0) {
      return check(false, "MCP config", `permissions are not private at ${target.path}; run chmod 600`);
    }
    const server = target.format === "json" ? readJSONServer(contents, target.rootKey, name) : readTOMLServer(contents, name);
    if (!server) return check(false, "MCP config", `server ${name} is missing from ${target.path}`);
    if (server.command !== "npx") return check(false, "MCP config", `server ${name} does not use npx`);
    if (!server.packageMatches) return check(false, "MCP config", `server ${name} does not use ${PACKAGE_SPECIFIER}`);
    if (!server.hasToken) return check(false, "MCP config", `server ${name} has no API token`);
    normalizeLocalAPIURL(server.apiUrl);
    return check(true, "MCP config", `valid at ${target.path}`);
  } catch (error) {
    return check(false, "MCP config", `invalid at ${target.path}: ${error.message}`);
  }
}

async function inspectSkill(target) {
  try {
    const contents = await fs.readFile(target.path, "utf8");
    if (!/^---[\s\S]*?^name:\s*aipermission-operator\s*$/m.test(contents)) {
      return check(false, "Operator skill", `invalid skill metadata at ${target.path}`);
    }
    return check(true, "Operator skill", `valid at ${target.path}`);
  } catch (error) {
    if (error.code === "ENOENT") return check(false, "Operator skill", `not found at ${target.path}`);
    return check(false, "Operator skill", `could not read ${target.path}: ${error.message}`);
  }
}

function readJSONServer(contents, rootKey, name) {
  const root = JSON.parse(contents);
  const server = root?.[rootKey]?.[name];
  if (!server || typeof server !== "object" || Array.isArray(server)) return null;
  return {
    command: server.command,
    packageMatches: Array.isArray(server.args) && server.args.includes(PACKAGE_SPECIFIER),
    hasToken: typeof server.env?.AIPERMISSION_API_TOKEN === "string" && server.env.AIPERMISSION_API_TOKEN.length > 0,
    apiUrl: server.env?.AIPERMISSION_API_URL,
  };
}

function readTOMLServer(contents, name) {
  const key = tomlKey(name);
  const mainHeader = `[mcp_servers.${key}]`;
  const nestedPrefix = `[mcp_servers.${key}.`;
  const lines = contents.split(/\r?\n/);
  const start = lines.findIndex((line) => line.trim() === mainHeader);
  if (start < 0) return null;
  const block = [];
  for (let index = start + 1; index < lines.length; index += 1) {
    const line = lines[index].trim();
    if (line.startsWith("[mcp_servers.") && line !== mainHeader && !line.startsWith(nestedPrefix)) break;
    block.push(lines[index]);
  }
  const command = tomlStringValue(block, "command");
  const apiUrl = tomlStringValue(block, "AIPERMISSION_API_URL");
  const token = tomlStringValue(block, "AIPERMISSION_API_TOKEN");
  return {
    command,
    packageMatches: block.some((line) => line.includes(JSON.stringify(PACKAGE_SPECIFIER))),
    hasToken: token.length > 0,
    apiUrl,
  };
}

function tomlStringValue(lines, key) {
  const prefix = `${key} = `;
  const line = lines.map((value) => value.trim()).find((value) => value.startsWith(prefix));
  if (!line) return "";
  const raw = line.slice(prefix.length);
  try {
    return JSON.parse(raw);
  } catch {
    return "";
  }
}

function check(ok, label, message) {
  return { ok, label, message };
}
