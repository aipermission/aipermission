import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { clientLabel, normalizeClientID, resolveSkillTarget } from "./client-registry.js";

const SKILL_NAME = "aipermission-operator";
const moduleDir = path.dirname(fileURLToPath(import.meta.url));

export async function runInstallSkill(argv = []) {
  const flags = parseFlags(argv);
  const client = normalizeClient(flags.client || "codex");
  const skill = await loadSkill(flags.source);
  const homeDir = flags.home || os.homedir();
  const projectDir = flags.projectDir || process.cwd();

  if (client === "custom") {
    console.log(renderInstruction("custom", skill));
    return;
  }

  const target = skillPathForClient(client, { homeDir, projectDir, scope: flags.scope });
  const content = renderInstruction(client, skill);
  await fs.mkdir(path.dirname(target.path), { recursive: true });
  await fs.writeFile(target.path, content, { mode: 0o644 });

  console.log(`Installed ${SKILL_NAME} skill for ${clientLabel(client)} (${target.scope}):`);
  console.log(target.path);
  console.log("");
  console.log("Restart the AI client or open a new session so the instructions refresh.");
}

export function codexSkillPath(homeDir) {
  return path.join(homeDir, ".agents", "skills", SKILL_NAME, "SKILL.md");
}

export function skillPathForClient(client, { homeDir = os.homedir(), projectDir = process.cwd(), scope } = {}) {
  const normalized = normalizeClient(client);
  return resolveSkillTarget(normalized, scope, { homeDir, projectDir });
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

function validateSkill(value) {
  if (!value.includes(`name: ${SKILL_NAME}`)) {
    throw new Error(`source does not look like ${SKILL_NAME}`);
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

function parseFlags(argv) {
  const result = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      continue;
    }
    const [rawKey, inlineValue] = arg.slice(2).split("=", 2);
    const key = rawKey.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
    result[key] = inlineValue ?? argv[i + 1] ?? "";
    if (inlineValue === undefined) {
      i += 1;
    }
  }
  return result;
}
