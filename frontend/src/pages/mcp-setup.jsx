import { PlugZap } from "lucide-react";
import { mcpApiUrl } from "../lib/api";
import { Badge } from "../components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { CopyButton } from "../components/ui/copy-button";
import { Notice } from "../components/ui/notice";
import { TerminalBlock } from "../components/ui/terminal-block";

const packageName = "@aipermission/mcp";
const initCommand = `npx -y ${packageName} init \\
  --provider codex \\
  --name aipermission-default`;
const skillInstallCommand = `npx -y ${packageName} install-skill --client codex
npx -y ${packageName} install-skill --client claude-code
npx -y ${packageName} install-skill --client cursor
npx -y ${packageName} install-skill --client vscode
npx -y ${packageName} install-skill --client windsurf
npx -y ${packageName} install-skill --client antigravity
npx -y ${packageName} install-skill --client gemini`;

const manualConfig = JSON.stringify(
  {
    mcpServers: {
      "aipermission-default": {
        command: "npx",
        args: ["-y", packageName],
        env: {
          NODE_ENV: "production",
          AIPERMISSION_API_URL: mcpApiUrl,
          AIPERMISSION_API_TOKEN: "TOKEN",
        },
      },
    },
  },
  null,
  2
);

const providers = [
  ["codex", "OpenAI Codex"],
  ["claude-code", "Claude Code"],
  ["cursor", "Cursor"],
  ["vscode", "VS Code"],
  ["windsurf", "Windsurf"],
  ["antigravity", "Google Antigravity"],
  ["gemini", "Gemini CLI"],
  ["custom", "Manual JSON"],
];

export function MCPSetupPage() {
  return (
    <section className="mx-auto grid w-full max-w-5xl gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">MCP setup</h3>
          <p className="text-sm text-stone-500">Install the npm MCP bridge, bind it to a token, then tell your AI which MCP server name to use.</p>
        </div>
        <Badge tone="good" className="gap-1">
          <PlugZap className="h-3.5 w-3.5" />
          {packageName}
        </Badge>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recommended install</CardTitle>
          <CardDescription>`npx` downloads the package from npm and runs init. The CLI asks for the API token with a hidden prompt.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <div className="grid gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3 text-sm text-stone-600">
            <Step number="1" title="Create or copy a token" text="Use the Tokens page. Copy the token at creation time, or enable reusable token copy in Settings before creating it." compact />
            <Step number="2" title="Run the init command" text="No global npm install is required; npx fetches @aipermission/mcp, asks for the token, and writes the provider config." compact />
            <Step number="3" title="Restart the AI client" text="Then tell the AI to use the configured MCP server name, for example aipermission-default." compact />
          </div>
          <CodeBlock value={initCommand} />
          <Notice>
            Project-local config files that are already tracked by Git are refused by default because they contain a bearer token. Use <span className="font-mono">--print</span> for manual copy, or <span className="font-mono">--force</span> only when you intentionally accept commit risk.
          </Notice>
          <Notice>
            Optional local install is only needed for development: <span className="font-mono">npm install @aipermission/mcp</span>. For automation, pipe the token with <span className="font-mono">--token-stdin</span>.
          </Notice>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Operator instructions</CardTitle>
          <CardDescription>Optional client-specific instructions for approval polling, running commands, console reads, reasons, and secret-safe command habits.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <div className="grid gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3 text-sm text-stone-600">
            <Step number="1" title="Choose your client" text="Run the matching command below from the workspace where your AI client works." compact />
            <Step number="2" title="Restart the AI client" text="Open a new session so the instruction or rule file is loaded." compact />
            <Step number="3" title="Use aipermission" text="Ask the AI to use the aipermission MCP server; the rule guides polling, live console reads, and safe command style." compact />
          </div>
          <CodeBlock value={skillInstallCommand} />
          <Notice>Supported: Codex skills, Claude Code rules, Cursor rules, VS Code Copilot instructions, Windsurf rules, Antigravity rules, and Gemini CLI context.</Notice>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-[0.85fr_1.15fr]">
        <Card>
          <CardHeader>
            <CardTitle>Providers</CardTitle>
            <CardDescription>Use the provider id with `--provider`.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-2">
            {providers.map(([id, label]) => (
              <div key={id} className="flex items-center justify-between rounded-md border border-stone-200 p-3 text-sm">
                <span className="font-medium text-stone-800">{label}</span>
                <Badge>{id}</Badge>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Manual JSON</CardTitle>
            <CardDescription>Use this for custom MCP clients that accept a JSON config.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4">
            <CodeBlock value={manualConfig} />
            <Notice>Use `TOKEN` as a placeholder in docs or examples; paste the real token only into your local MCP client config.</Notice>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>How it works</CardTitle>
          <CardDescription>The MCP server is a tiny stdio bridge. It never receives SSH private keys.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 text-sm text-stone-600">
          <Step number="1" title="Create a token" text="Each AI client or agent should get its own token." />
          <Step number="2" title="Run init" text="The CLI writes the provider-specific MCP config using that token." />
          <Step number="3" title="Grant permissions" text="Use Console or Tokens to choose which connector target actions are disabled, prompt, or always run." />
          <Step number="4" title="Use the MCP name" text="Tell the AI to use the configured MCP server, for example `aipermission-default`." />
        </CardContent>
      </Card>
    </section>
  );
}

function CodeBlock({ value }) {
  return (
    <div className="grid gap-2">
      <div className="flex justify-end">
        <CopyButton value={value} variant="outline" className="h-9 px-3" />
      </div>
      <TerminalBlock className="whitespace-pre">{value}</TerminalBlock>
    </div>
  );
}

function Step({ number, title, text, compact = false }) {
  return (
    <div className={`flex gap-3 rounded-md ${compact ? "" : "border border-stone-200 p-3"}`}>
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-emerald-950 text-xs font-bold text-white">{number}</span>
      <div>
        <p className="font-semibold text-stone-900">{title}</p>
        <p className="mt-1">{text}</p>
      </div>
    </div>
  );
}
