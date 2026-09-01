import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseDocument } from "yaml";
import { parseCommandFlags } from "./cli-flags.js";
import { clientLabel, normalizeClientID, resolveSkillTarget } from "./client-registry.js";
import { atomicWriteTrustedFile, prepareTrustedFileDestination, withPrivateFileLock } from "./private-file.js";

const SKILL_NAME = "aipermission-operator";
const moduleDir = path.dirname(fileURLToPath(import.meta.url));

export async function runInstallSkill(argv = []) {
  const flags = parseCommandFlags("install-skill", argv);
  const result = await installSkill({
    client: flags.client || "codex",
    scope: flags.scope,
    source: flags.source,
    homeDir: flags.home,
    projectDir: flags.projectDir,
  });

  if (!result.path) {
    console.log(result.content);
    return;
  }

  console.log(`Installed ${SKILL_NAME} skill for ${clientLabel(result.client)} (${result.scope}):`);
  console.log(result.path);
  console.log("");
  console.log("Restart the AI client or open a new session so the instructions refresh.");
}

export async function installSkill({ client = "codex", scope, source, homeDir, projectDir } = {}) {
  const prepared = await prepareSkillInstallation({ client, scope, source, homeDir, projectDir });
  return commitSkillInstallation(prepared);
}

export async function commitSkillInstallation(prepared) {
  if (!prepared.path) return prepared;
  await withPrivateFileLock(
    prepared.path,
    () => atomicWriteTrustedFile(prepared.path, prepared.content, { trustedRoot: prepared.trustedRoot }),
    { trustedRoot: prepared.trustedRoot },
  );
  return withoutTrustedRoot(prepared);
}

export async function prepareSkillInstallation({ client = "codex", scope, source, homeDir, projectDir } = {}) {
  const normalized = normalizeClient(client);
  const content = renderInstruction(normalized, await loadSkill(source));
  if (normalized === "custom") {
    return { client: normalized, content, path: "", scope: "" };
  }
  const target = skillPathForClient(normalized, { homeDir, projectDir, scope });
  const trustedRoot = target.trustedRoot;
  await prepareTrustedFileDestination(target.path, { trustedRoot });
  return { client: normalized, content, path: target.path, scope: target.scope, trustedRoot };
}

export function codexSkillPath(homeDir) {
  return resolveSkillTarget("codex", "user", { homeDir }).path;
}

export function skillPathForClient(client, { homeDir, projectDir, scope, env } = {}) {
  const normalized = normalizeClient(client);
  return resolveSkillTarget(normalized, scope, { homeDir, projectDir, env });
}

export async function loadSkill(source) {
  if (source) {
    return readSkillSource(source);
  }
  const errors = [];
  for (const candidate of bundledSkillCandidates()) {
    try {
      return await readSkillSource(candidate);
    } catch (error) {
      errors.push(`${candidate}: ${error.message}`);
    }
  }
  throw new Error(`Could not load bundled ${SKILL_NAME} skill.\n${errors.join("\n")}`);
}

function bundledSkillCandidates() {
  return [path.join(moduleDir, "resources", SKILL_NAME, "SKILL.md"), path.join(moduleDir, "..", "resources", SKILL_NAME, "SKILL.md")];
}

async function readSkillSource(source) {
  if (/^https?:\/\//i.test(source)) {
    throw new Error("remote skill sources are not supported; use the bundled skill or a local file path");
  }
  return validateSkill(await fs.readFile(source, "utf8"));
}

export function validateSkill(value) {
  const match = String(value).match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  if (!match) throw new Error(`${SKILL_NAME} source must start with closed YAML frontmatter`);
  const document = parseDocument(match[1]);
  if (document.errors.length > 0) throw new Error(`${SKILL_NAME} frontmatter is invalid`);
  const metadata = document.toJS();
  if (!metadata || typeof metadata !== "object" || metadata.name !== SKILL_NAME) {
    throw new Error(`${SKILL_NAME} frontmatter must declare the exact skill name`);
  }
  if (typeof metadata.description !== "string" || !metadata.description.trim()) {
    throw new Error(`${SKILL_NAME} frontmatter must include a description`);
  }
  if (!String(value).slice(match[0].length).trim()) {
    throw new Error(`${SKILL_NAME} source must include instructions after frontmatter`);
  }
  return value;
}

export function renderInstruction(client, skill) {
  normalizeClient(client);
  return skill.endsWith("\n") ? skill : `${skill}\n`;
}

export function normalizeClient(value) {
  return normalizeClientID(value);
}

function withoutTrustedRoot(prepared) {
  const { trustedRoot: _trustedRoot, ...result } = prepared;
  return result;
}
