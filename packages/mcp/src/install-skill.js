import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { clientLabel, normalizeClientID } from "./client-registry.js";

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

  const targetPath = skillPathForClient(client, { homeDir, projectDir });
  const content = renderInstruction(client, skill);
  if (client === "gemini") {
    await upsertMarkedSection(targetPath, content);
  } else {
    await fs.mkdir(path.dirname(targetPath), { recursive: true });
    await fs.writeFile(targetPath, content, { mode: 0o644 });
  }

  console.log(`Installed ${SKILL_NAME} instructions for ${clientLabel(client)}:`);
  console.log(targetPath);
  console.log("");
  console.log("Restart the AI client or open a new session so the instructions refresh.");
}

export function codexSkillPath(homeDir) {
  return path.join(homeDir, ".codex", "skills", SKILL_NAME, "SKILL.md");
}

export function skillPathForClient(client, { homeDir = os.homedir(), projectDir = process.cwd() } = {}) {
  const normalized = normalizeClient(client);
  if (normalized === "codex") {
    return codexSkillPath(homeDir);
  }
  if (normalized === "claude-code") {
    return path.join(projectDir, ".claude", "rules", `${SKILL_NAME}.md`);
  }
  if (normalized === "cursor") {
    return path.join(projectDir, ".cursor", "rules", `${SKILL_NAME}.mdc`);
  }
  if (normalized === "vscode") {
    return path.join(projectDir, ".github", "instructions", `${SKILL_NAME}.instructions.md`);
  }
  if (normalized === "windsurf") {
    return path.join(projectDir, ".windsurf", "rules", `${SKILL_NAME}.md`);
  }
  if (normalized === "antigravity") {
    return path.join(projectDir, ".agents", "rules", `${SKILL_NAME}.md`);
  }
  if (normalized === "gemini") {
    return path.join(projectDir, "GEMINI.md");
  }
  throw new Error(`Unsupported client: ${client}`);
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
  const normalized = normalizeClient(client);
  if (normalized === "codex") {
    return skill;
  }

  const body = stripSkillFrontmatter(skill).trim();
  if (normalized === "claude-code") {
    return `${body}\n`;
  }
  if (normalized === "cursor") {
    return `---\ndescription: AIPermission MCP operator workflow for approval polling, console reads, reasons, and secret-safe commands.\nglobs:\nalwaysApply: true\n---\n\n${body}\n`;
  }
  if (normalized === "vscode") {
    return `---\nname: AIPermission Operator\ndescription: Use AIPermission MCP safely with approvals, console reads, reasons, and secret hygiene.\napplyTo: "**"\n---\n\n${body}\n`;
  }
  if (normalized === "windsurf") {
    return `---\ntrigger: always_on\n---\n\n${body}\n`;
  }
  if (normalized === "antigravity") {
    return `---\ndescription: AIPermission MCP operator workflow\ntrigger: always_on\n---\n\n${body}\n`;
  }
  if (normalized === "gemini") {
    return `## AIPermission Operator\n\n${body.replace(/^# AIPermission Operator\s*/m, "").trim()}\n`;
  }
  if (normalized === "custom") {
    return `${body}\n`;
  }
  throw new Error(`Unsupported client: ${client}`);
}

export function normalizeClient(value) {
  return normalizeClientID(value);
}

function stripSkillFrontmatter(value) {
  return value.replace(/^---\n[\s\S]*?\n---\n?/, "");
}

async function upsertMarkedSection(filePath, content) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  const start = "<!-- aipermission-operator:start -->";
  const end = "<!-- aipermission-operator:end -->";
  const section = `${start}\n${content.trim()}\n${end}\n`;
  let existing = "";
  try {
    existing = await fs.readFile(filePath, "utf8");
  } catch (error) {
    if (error.code !== "ENOENT") {
      throw error;
    }
  }
  const pattern = new RegExp(`${escapeRegExp(start)}[\\s\\S]*?${escapeRegExp(end)}\\n?`);
  const next = pattern.test(existing)
    ? existing.replace(pattern, section)
    : `${existing.replace(/\s*$/, "")}${existing.trim() ? "\n\n" : ""}${section}`;
  await fs.writeFile(filePath, next, { mode: 0o644 });
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
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
