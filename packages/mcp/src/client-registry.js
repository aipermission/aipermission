const clients = [
  {
    id: "codex",
    label: "OpenAI Codex",
    aliases: [],
    providerDescription: "Writes ~/.codex/config.toml",
  },
  {
    id: "claude-code",
    label: "Claude Code",
    aliases: ["claude", "claude_code"],
    providerDescription: "Writes .mcp.json in the current project",
  },
  {
    id: "cursor",
    label: "Cursor",
    aliases: [],
    providerDescription: "Writes .cursor/mcp.json in the current project",
  },
  {
    id: "vscode",
    label: "VS Code / GitHub Copilot",
    aliases: ["copilot", "vs-code"],
    providerDescription: "Writes .vscode/mcp.json in the current project",
  },
  {
    id: "windsurf",
    label: "Windsurf",
    aliases: [],
    providerDescription: "Writes ~/.codeium/windsurf/mcp_config.json",
  },
  {
    id: "antigravity",
    label: "Google Antigravity",
    aliases: ["google-antigravity", "agy"],
    providerDescription: "Writes ~/.gemini/antigravity/mcp_config.json",
  },
  {
    id: "gemini",
    label: "Gemini CLI",
    aliases: ["gemini-cli"],
    providerDescription: "Writes ~/.gemini/settings.json",
  },
  {
    id: "custom",
    label: "Custom / copy-paste",
    aliases: [],
    providerDescription: "Prints config snippets only",
  },
];

export const MCP_PROVIDERS = clients.map(({ id, label, providerDescription }) => ({
  id,
  label,
  description: providerDescription,
}));

export function getClient(value) {
  const normalized = normalizeClientID(value);
  return clients.find((client) => client.id === normalized);
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
