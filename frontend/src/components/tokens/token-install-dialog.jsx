import { mcpApiUrl } from "../../lib/api";
import { CopyButton } from "../ui/copy-button";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";
import { TerminalBlock } from "../ui/terminal-block";

export const installProviders = [
  { id: "manual", label: "Manual" },
  { id: "claude-code", label: "Claude" },
  { id: "codex", label: "Codex" },
  { id: "cursor", label: "Cursor" },
  { id: "windsurf", label: "Windsurf" },
  { id: "vscode", label: "VSCode" },
  { id: "antigravity", label: "Antigravity" },
  { id: "gemini", label: "Gemini" },
  { id: "custom", label: "Custom" },
];
export function TokenInstallDialog({ state, onChange, onClose }) {
  const token = state.token;
  const provider = state.provider || "manual";
  const manualConfig = provider === "manual";
  const customConfig = provider === "custom";
  const targetName = token ? installTargetName(token.name) : "aipermission-default";
  const command = token ? installCommand(provider, targetName, token.token) : "";
  const manualJSON = token ? manualConfigJSON(targetName, token.token) : "";
  const copyValue = manualConfig ? manualJSON : command;
  const providerLabel = installProviders.find((item) => item.id === provider)?.label || "Manual";

  return (
    <Dialog
      open={state.open}
      title={token ? `Install ${token.name}` : "Install MCP"}
      description="Copy the setup command for the AI client that should use this token."
      onClose={onClose}
      size="xl"
      bodyClassName="grid gap-4"
    >
      {token?.token ? (
        <>
          <div className="flex flex-wrap gap-2 rounded-lg border border-stone-200 bg-stone-100 p-1">
            {installProviders.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`h-9 rounded-md px-3 text-sm font-semibold transition ${
                  provider === item.id ? "bg-white text-emerald-950 shadow-sm" : "text-stone-500 hover:text-stone-900"
                }`}
                onClick={() => onChange((current) => ({ ...current, provider: item.id }))}
              >
                {item.label}
              </button>
            ))}
          </div>

          <div className="grid gap-2">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-semibold text-stone-900">{providerLabel} setup</p>
                <p className="text-xs text-stone-500">
                  MCP server name: <span className="font-mono">{targetName}</span>
                </p>
              </div>
              <CopyButton value={copyValue} variant="outline" />
            </div>
            <TerminalBlock className="max-h-[420px] whitespace-pre">{copyValue}</TerminalBlock>
          </div>

          <Notice>
            {manualConfig
              ? "Manual config includes the token for copy-paste setup. Keep the config file private."
              : customConfig
                ? "Custom prints portable config instead of writing provider files. Keep the generated config private."
                : "The init command asks for the token with a hidden prompt. After installing, tell the AI to use the "}
            {!manualConfig && !customConfig ? <span className="font-mono">{targetName}</span> : null}
            {!manualConfig && !customConfig ? " MCP server for this task." : null}
          </Notice>
        </>
      ) : token ? (
        <Notice tone="warn">This token value is not available for copy. Create a new token, or enable reusable token copy in Settings before creating tokens.</Notice>
      ) : null}
    </Dialog>
  );
}
function installTargetName(value) {
  const slug = String(value || "default")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_.-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `aipermission-${slug || "default"}`;
}

function installCommand(provider, name, token) {
  const printFlag = provider === "custom" ? " \\\n  --print" : "";
  return `npx -y @aipermission/mcp init \\
  --provider ${provider} \\
  --name ${name}${printFlag}`;
}

function manualConfigJSON(name, token) {
  return JSON.stringify(
    {
      mcpServers: {
        [name]: {
          command: "npx",
          args: ["-y", "@aipermission/mcp"],
          env: {
            NODE_ENV: "production",
            AIPERMISSION_API_URL: mcpApiUrl,
            AIPERMISSION_API_TOKEN: token,
          },
        },
      },
    },
    null,
    2
  );
}
