import os from "node:os";
import path from "node:path";

const clients = [
  {
    id: "codex",
    label: "OpenAI Codex",
    aliases: [],
    mcp: mcpConfig("user", {
      user: tomlTarget("home", ".codex/config.toml"),
      project: tomlTarget("project", ".codex/config.toml"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".agents/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  },
  {
    id: "claude-code",
    label: "Claude Code",
    aliases: ["claude", "claude_code"],
    mcp: mcpConfig("project", {
      user: jsonTarget("home", ".claude.json", "mcpServers"),
      project: jsonTarget("project", ".mcp.json", "mcpServers"),
    }),
    skill: skillConfig("project", {
      user: skillTarget("home", ".claude/skills"),
      project: skillTarget("project", ".claude/skills"),
    }),
  },
  {
    id: "cursor",
    label: "Cursor",
    aliases: [],
    mcp: mcpConfig("project", {
      user: jsonTarget("home", ".cursor/mcp.json", "mcpServers"),
      project: jsonTarget("project", ".cursor/mcp.json", "mcpServers"),
    }),
    skill: skillConfig("project", {
      user: skillTarget("home", ".cursor/skills"),
      project: skillTarget("project", ".cursor/skills"),
    }),
  },
  {
    id: "vscode",
    label: "VS Code",
    aliases: ["vs-code"],
    mcp: mcpConfig("project", {
      project: jsonTarget("project", ".vscode/mcp.json", "servers"),
    }),
    skill: skillConfig("project", {
      user: skillTarget("home", ".copilot/skills"),
      project: skillTarget("project", ".github/skills"),
    }),
  },
  {
    id: "copilot",
    label: "GitHub Copilot CLI",
    aliases: ["copilot-cli", "github-copilot"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".copilot/mcp-config.json", "mcpServers"),
      project: jsonTarget("project", ".mcp.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".copilot/skills"),
      project: skillTarget("project", ".github/skills"),
    }),
  },
  {
    id: "windsurf",
    label: "Windsurf Cascade (legacy)",
    aliases: ["windsurf-legacy", "cascade"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".codeium/windsurf/mcp_config.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".codeium/windsurf/skills"),
      project: skillTarget("project", ".windsurf/skills"),
    }),
  },
  {
    id: "antigravity",
    label: "Google Antigravity",
    aliases: ["google-antigravity", "agy"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".gemini/config/mcp_config.json", "mcpServers"),
      project: jsonTarget("project", ".agents/mcp_config.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".gemini/config/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  },
  {
    id: "gemini",
    label: "Gemini CLI",
    aliases: ["gemini-cli"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".gemini/settings.json", "mcpServers"),
      project: jsonTarget("project", ".gemini/settings.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".gemini/skills"),
      project: skillTarget("project", ".gemini/skills"),
    }),
  },
  {
    id: "grok",
    label: "Grok Build CLI",
    aliases: ["grok-build", "grok-cli"],
    mcp: mcpConfig("user", {
      user: tomlTarget("home", ".grok/config.toml"),
      project: tomlTarget("project", ".grok/config.toml"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".grok/skills"),
      project: skillTarget("project", ".grok/skills"),
    }),
  },
  {
    id: "agents",
    label: "Agents standard",
    aliases: ["agents-standard"],
    mcp: null,
    skill: skillConfig("project", {
      user: skillTarget("home", ".agents/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  },
  {
    id: "custom",
    label: "Custom / copy-paste",
    aliases: [],
    mcp: null,
    skill: null,
  },
];

export const MCP_PROVIDERS = clients
  .filter((client) => client.mcp || client.id === "custom")
  .map((client) => ({
    id: client.id,
    label: client.label,
    description: providerDescription(client),
  }));

export function getClient(value) {
  const normalized = normalizeClientID(value);
  return clients.find((client) => client.id === normalized);
}

export function resolveSkillTarget(clientValue, scopeValue, { homeDir = os.homedir(), projectDir = process.cwd() } = {}) {
  const client = getClient(clientValue);
  if (!client.skill) {
    throw new Error(`${client.label} does not have an automatic skill target.`);
  }
  const scope = normalizeScope(scopeValue || client.skill.defaultScope);
  const target = client.skill.scopes[scope];
  if (!target) {
    const supported = Object.keys(client.skill.scopes).join(", ");
    throw new Error(`${client.label} does not support ${scope} skill scope. Supported scopes: ${supported}.`);
  }
  const baseDir = path.resolve(target.base === "home" ? homeDir : projectDir);
  return {
    ...target,
    client: client.id,
    label: client.label,
    scope,
    path: path.join(baseDir, ...target.segments, "aipermission-operator", "SKILL.md"),
  };
}

export function normalizeClientID(value) {
  const candidate = String(value || "")
    .trim()
    .toLowerCase();
  const client = clients.find(
    (item) => item.id === candidate || item.label.toLowerCase() === candidate || item.aliases.includes(candidate),
  );
  if (!client) {
    throw new Error(`Unknown client: ${value}`);
  }
  return client.id;
}

export function clientLabel(value) {
  return getClient(value).label;
}

export function resolveMCPConfigTarget(clientValue, scopeValue, { homeDir = os.homedir(), projectDir = process.cwd() } = {}) {
  const client = getClient(clientValue);
  if (!client.mcp) {
    throw new Error(`${client.label} does not have an automatic MCP config target.`);
  }
  const scope = normalizeScope(scopeValue || client.mcp.defaultScope);
  const target = client.mcp.scopes[scope];
  if (!target) {
    const supported = Object.keys(client.mcp.scopes).join(", ");
    throw new Error(`${client.label} does not support ${scope} MCP scope. Supported scopes: ${supported}.`);
  }
  const baseDir = path.resolve(target.base === "home" ? homeDir : projectDir);
  return {
    ...target,
    client: client.id,
    label: client.label,
    scope,
    path: path.join(baseDir, ...target.segments),
    projectConfig: target.base === "project",
  };
}

export function normalizeScope(value) {
  const scope = String(value || "")
    .trim()
    .toLowerCase();
  if (scope !== "user" && scope !== "project") {
    throw new Error(`Unknown scope: ${value}. Use user or project.`);
  }
  return scope;
}

function mcpConfig(defaultScope, scopes) {
  return { defaultScope, scopes };
}

function skillConfig(defaultScope, scopes) {
  return { defaultScope, scopes };
}

function jsonTarget(base, relativePath, rootKey) {
  return { base, segments: relativePath.split("/"), format: "json", rootKey };
}

function tomlTarget(base, relativePath) {
  return { base, segments: relativePath.split("/"), format: "toml", rootKey: "mcp_servers" };
}

function skillTarget(base, relativePath) {
  return { base, segments: relativePath.split("/") };
}

function providerDescription(client) {
  if (!client.mcp) {
    return "Prints config snippets only";
  }
  const target = client.mcp.scopes[client.mcp.defaultScope];
  const prefix = target.base === "home" ? "~/" : "./";
  return `Writes ${prefix}${target.segments.join("/")} (${client.mcp.defaultScope})`;
}
