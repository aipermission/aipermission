import os from "node:os";
import path from "node:path";

const clients = [
  client("codex", "OpenAI Codex", {
    mcp: mcpConfig("user", {
      user: tomlTarget("home", ".codex/config.toml", { homeEnv: "CODEX_HOME", envPath: "config.toml" }),
      project: tomlTarget("project", ".codex/config.toml"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".agents/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  }),
  client("claude-code", "Claude Code", {
    aliases: ["claude", "claude_code"],
    mcp: mcpConfig("project", {
      user: jsonTarget("home", ".claude.json", "mcpServers"),
      project: jsonTarget("project", ".mcp.json", "mcpServers"),
    }),
    skill: skillConfig("project", {
      user: skillTarget("home", ".claude/skills", { homeEnv: "CLAUDE_CONFIG_DIR", envPath: "skills" }),
      project: skillTarget("project", ".claude/skills"),
    }),
  }),
  client("cursor", "Cursor", {
    mcp: mcpConfig("project", {
      user: jsonTarget("home", ".cursor/mcp.json", "mcpServers"),
      project: jsonTarget("project", ".cursor/mcp.json", "mcpServers"),
    }),
    skill: skillConfig("project", {
      user: skillTarget("home", ".cursor/skills"),
      project: skillTarget("project", ".cursor/skills"),
    }),
  }),
  client("vscode", "VS Code", {
    aliases: ["vs-code"],
    mcp: mcpConfig(
      "project",
      { project: jsonTarget("project", ".vscode/mcp.json", "servers") },
      { printScopes: { user: virtualJSONTarget("servers") } },
    ),
    skill: skillConfig("project", {
      user: skillTarget("home", ".copilot/skills"),
      project: skillTarget("project", ".github/skills"),
    }),
  }),
  client("copilot", "GitHub Copilot CLI", {
    aliases: ["copilot-cli", "github-copilot"],
    mcp: mcpConfig(
      "user",
      {
        user: jsonTarget("home", ".copilot/mcp-config.json", "mcpServers", {
          homeEnv: "COPILOT_HOME",
          envPath: "mcp-config.json",
        }),
        project: jsonTarget("project", ".mcp.json", "mcpServers"),
      },
      { serverExtras: { type: "local", tools: ["*"] } },
    ),
    skill: skillConfig("user", {
      user: skillTarget("home", ".copilot/skills", { homeEnv: "COPILOT_HOME", envPath: "skills" }),
      project: skillTarget("project", ".github/skills"),
    }),
  }),
  client("windsurf", "Windsurf Cascade (legacy)", {
    aliases: ["windsurf-legacy", "cascade"],
    mcp: mcpConfig("user", { user: jsonTarget("home", ".codeium/windsurf/mcp_config.json", "mcpServers") }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".codeium/windsurf/skills"),
      project: skillTarget("project", ".windsurf/skills"),
    }),
  }),
  client("antigravity", "Google Antigravity IDE", {
    aliases: ["google-antigravity"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".gemini/config/mcp_config.json", "mcpServers"),
      project: jsonTarget("project", ".agents/mcp_config.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".gemini/config/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  }),
  client("antigravity-cli", "Google Antigravity CLI", {
    aliases: ["agy"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".gemini/antigravity-cli/mcp_config.json", "mcpServers"),
      project: jsonTarget("project", ".agents/mcp_config.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".gemini/antigravity-cli/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  }),
  client("gemini", "Gemini CLI", {
    aliases: ["gemini-cli"],
    mcp: mcpConfig("user", {
      user: jsonTarget("home", ".gemini/settings.json", "mcpServers", {
        homeEnv: "GEMINI_CLI_HOME",
        envPath: "settings.json",
      }),
      project: jsonTarget("project", ".gemini/settings.json", "mcpServers"),
    }),
    skill: skillConfig("user", {
      user: skillTarget("home", ".gemini/skills", { homeEnv: "GEMINI_CLI_HOME", envPath: "skills" }),
      project: skillTarget("project", ".gemini/skills"),
    }),
  }),
  client("grok", "Grok Build CLI", {
    aliases: ["grok-build", "grok-cli"],
    mcp: mcpConfig(
      "user",
      {
        user: tomlTarget("home", ".grok/config.toml", { homeEnv: "GROK_HOME", envPath: "config.toml" }),
        project: tomlTarget("project", ".grok/config.toml"),
      },
      { serverExtras: { startup_timeout_sec: 60 } },
    ),
    skill: skillConfig("user", {
      user: skillTarget("home", ".grok/skills", { homeEnv: "GROK_HOME", envPath: "skills" }),
      project: skillTarget("project", ".grok/skills"),
    }),
  }),
  client("agents", "Agents standard", {
    aliases: ["agents-standard"],
    skill: skillConfig("project", {
      user: skillTarget("home", ".agents/skills"),
      project: skillTarget("project", ".agents/skills"),
    }),
  }),
  client("custom", "Custom / copy-paste"),
];

export const MCP_PROVIDERS = clients
  .filter((entry) => entry.mcp || entry.id === "custom")
  .map((entry) => ({ id: entry.id, label: entry.label, description: providerDescription(entry) }));

export function getClient(value) {
  const normalized = normalizeClientID(value);
  return clients.find((entry) => entry.id === normalized);
}

export function getClientCatalog() {
  return clients.map(({ id, label, aliases, mcp, skill }) => ({
    id,
    label,
    aliases: [...aliases],
    supportsMCP: Boolean(mcp),
    supportsSkill: Boolean(skill),
  }));
}

export function adaptMCPServerConfig(clientValue, config) {
  const clientEntry = getClient(clientValue);
  return { ...config, ...(clientEntry.mcp?.serverExtras || {}) };
}

export function resolveSkillTarget(clientValue, scopeValue, options = {}) {
  return resolveTarget(clientValue, "skill", scopeValue, options, "skill");
}

export function resolveMCPConfigTarget(clientValue, scopeValue, options = {}) {
  return resolveTarget(clientValue, "mcp", scopeValue, options, "MCP config");
}

export function resolveMCPPrintTarget(clientValue, scopeValue, options = {}) {
  const clientEntry = getClient(clientValue);
  if (!clientEntry.mcp) throw new Error(`${clientEntry.label} does not have an MCP config format.`);
  const scope = normalizeScope(scopeValue || clientEntry.mcp.defaultScope);
  const target = clientEntry.mcp.scopes[scope] || clientEntry.mcp.printScopes?.[scope];
  if (!target) return resolveMCPConfigTarget(clientValue, scope, options);
  return resolvedTarget(clientEntry, target, scope, options);
}

export function normalizeClientID(value) {
  const candidate = String(value || "")
    .trim()
    .toLowerCase();
  const clientEntry = clients.find(
    (entry) => entry.id === candidate || entry.label.toLowerCase() === candidate || entry.aliases.includes(candidate),
  );
  if (!clientEntry) throw new Error(`Unknown client: ${value}`);
  return clientEntry.id;
}

export function clientLabel(value) {
  return getClient(value).label;
}

export function normalizeScope(value) {
  const scope = String(value || "")
    .trim()
    .toLowerCase();
  if (scope !== "user" && scope !== "project") throw new Error(`Unknown scope: ${value}. Use user or project.`);
  return scope;
}

function resolveTarget(clientValue, capability, scopeValue, options, capabilityLabel) {
  const clientEntry = getClient(clientValue);
  const config = clientEntry[capability];
  if (!config) throw new Error(`${clientEntry.label} does not have an automatic ${capabilityLabel} target.`);
  const scope = normalizeScope(scopeValue || config.defaultScope);
  const target = config.scopes[scope];
  if (!target) {
    const supported = Object.keys(config.scopes).join(", ");
    throw new Error(`${clientEntry.label} does not support ${scope} ${capabilityLabel} scope. Supported scopes: ${supported}.`);
  }
  return resolvedTarget(clientEntry, target, scope, options);
}

function resolvedTarget(clientEntry, target, scope, options) {
  const usesHomeOverride =
    target.base === "home" && !options.homeDir && Boolean(target.homeEnv && (options.env || process.env)[target.homeEnv]);
  const root = target.base === "project" ? path.resolve(options.projectDir || process.cwd()) : resolveHomeRoot(target, options);
  return {
    ...target,
    client: clientEntry.id,
    label: clientEntry.label,
    scope,
    path: target.virtual ? "" : path.join(root, ...(usesHomeOverride ? target.envSegments : target.segments)),
    trustedRoot: root,
    projectConfig: target.base === "project",
  };
}

function resolveHomeRoot(target, options) {
  if (options.homeDir) return path.resolve(options.homeDir);
  const overridden = target.homeEnv && (options.env || process.env)[target.homeEnv];
  return path.resolve(overridden || os.homedir());
}

function client(id, label, options = {}) {
  return { id, label, aliases: options.aliases || [], mcp: options.mcp || null, skill: options.skill || null };
}

function mcpConfig(defaultScope, scopes, options = {}) {
  return { defaultScope, scopes, printScopes: options.printScopes || {}, serverExtras: options.serverExtras || {} };
}

function skillConfig(defaultScope, scopes) {
  return { defaultScope, scopes };
}

function jsonTarget(base, relativePath, rootKey, options = {}) {
  return target(base, relativePath, { ...options, format: "json", rootKey });
}

function virtualJSONTarget(rootKey) {
  return { base: "home", segments: [], envSegments: [], format: "json", rootKey, virtual: true };
}

function tomlTarget(base, relativePath, options = {}) {
  return target(base, relativePath, { ...options, format: "toml", rootKey: "mcp_servers" });
}

function skillTarget(base, relativePath, options = {}) {
  const suffix = "aipermission-operator/SKILL.md";
  return target(base, `${relativePath}/${suffix}`, {
    ...options,
    envPath: options.envPath ? `${options.envPath}/${suffix}` : undefined,
  });
}

function target(base, relativePath, options = {}) {
  return {
    base,
    segments: relativePath.split("/"),
    homeEnv: options.homeEnv,
    envSegments: (options.envPath || relativePath).split("/"),
    format: options.format,
    rootKey: options.rootKey,
  };
}

function providerDescription(clientEntry) {
  if (!clientEntry.mcp) return "Prints config snippets only";
  const targetEntry = clientEntry.mcp.scopes[clientEntry.mcp.defaultScope];
  const prefix = targetEntry.base === "home" ? "~/" : "./";
  return `Writes ${prefix}${targetEntry.segments.join("/")} (${clientEntry.mcp.defaultScope})`;
}
