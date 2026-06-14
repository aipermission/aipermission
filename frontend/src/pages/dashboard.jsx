import { Activity, Cable, History, KeyRound, PlugZap, TerminalSquare, TicketCheck } from "lucide-react";
import { Link } from "react-router-dom";
import { apiUrl, mcpApiUrl } from "../lib/api";
import { Badge } from "../components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { useGateway } from "../lib/gateway-context";

export function DashboardPage() {
  const { targets, credentials, tokens, gatewayState } = useGateway();
  const activeTokens = tokens.data.filter((token) => !token.revoked_at).length;

  return (
    <section className="grid gap-5">
      <section className="grid gap-4 md:grid-cols-3">
        <Metric to="/connectors" icon={Cable} title="Connectors" value={targets.data.length} detail="registered targets" />
        <Metric to="/tokens" icon={TicketCheck} title="Tokens" value={activeTokens} detail="active MCP/API tokens" />
        <Metric to="/credentials" icon={KeyRound} title="Credentials" value={credentials.data.length} detail="gateway-owned profiles" />
      </section>

      <section className="grid gap-5 xl:grid-cols-[1fr_0.85fr]">
        <Card>
          <CardHeader>
            <CardTitle>Getting Started</CardTitle>
            <CardDescription>Small path, clear control: create a credential, add a connector, give a token permission, then let the AI work through MCP.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
            <LifecycleStep number="1" to="/credentials" icon={KeyRound} title="Create a credential" text="Generate or import a connector credential. aipermission keeps private material local and encrypted." />
            <LifecycleStep number="2" to="/connectors" icon={Cable} title="Add a connector" text="Create a connector target, attach a credential profile, then test it through the connector pipeline." />
            <LifecycleStep number="3" to="/tokens" icon={TicketCheck} title="Create a token and install MCP" text="Create one token per AI client or session, then use Install to copy the provider-specific init command." />
            <LifecycleStep number="4" to="/console" icon={TerminalSquare} title="Grant connector permission" text="Open Console, select a target and token, then choose disabled, prompt, or always." />
            <LifecycleStep number="5" to="/mcp-setup" icon={PlugZap} title="Use it with your AI" text="Tell the AI which MCP server name to use, such as aipermission-default." />
            <LifecycleStep number="6" to="/history" icon={History} title="Review history" text="Inspect executed commands, reasons, outputs, approvals, and failures after the session." />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Runtime</CardTitle>
            <CardDescription>Local gateway status and setup shape.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 text-sm text-stone-600">
            <div className="flex items-center justify-between rounded-md border border-stone-200 p-3">
              <span className="flex items-center gap-2">
                <Activity className="h-4 w-4 text-stone-500" />
                Gateway
              </span>
              <Badge tone={gatewayState === "running" ? "good" : "warn"}>{gatewayState}</Badge>
            </div>
            <div className="rounded-md border border-stone-200 p-3">
              <p className="text-xs font-semibold uppercase text-stone-500">API URL</p>
              <p className="mt-1 truncate font-mono text-xs text-stone-700">{apiUrl}</p>
            </div>
            <div className="rounded-md border border-stone-200 p-3">
              <p className="text-xs font-semibold uppercase text-stone-500">MCP URL</p>
              <p className="mt-1 truncate font-mono text-xs text-stone-700">{mcpApiUrl}</p>
            </div>
            <div className="flex items-center justify-between rounded-md border border-stone-200 p-3">
              <span>Credential setup</span>
              <Badge tone="good">local vault</Badge>
            </div>
            <div className="flex items-center justify-between rounded-md border border-stone-200 p-3">
              <span>MCP package</span>
              <Badge>@aipermission/mcp</Badge>
            </div>
            <div className="flex items-center justify-between rounded-md border border-stone-200 p-3">
              <span>Review surface</span>
              <Badge tone="neutral">History</Badge>
            </div>
          </CardContent>
        </Card>
      </section>
    </section>
  );
}

function Metric({ to, icon: Icon, title, value, detail }) {
  return (
    <Link to={to} className="block rounded-lg outline-none transition hover:-translate-y-0.5 focus:ring-2 focus:ring-emerald-900/20">
      <Card className="h-full">
        <CardContent className="p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="rounded-md bg-stone-100 p-2 text-stone-700">
              <Icon className="h-5 w-5" />
            </div>
            <Badge tone="good">{value}</Badge>
          </div>
          <h3 className="mt-4 text-sm font-semibold text-stone-950">{title}</h3>
          <p className="mt-1 truncate text-sm text-stone-500">{detail}</p>
        </CardContent>
      </Card>
    </Link>
  );
}

function LifecycleStep({ number, to, icon: Icon, title, text }) {
  return (
    <Link to={to} className="group flex gap-3 rounded-lg border border-stone-200 p-3 transition hover:border-emerald-800 hover:bg-emerald-50/40">
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-emerald-950 text-xs font-bold text-white">{number}</span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Icon className="h-4 w-4 text-stone-500 group-hover:text-emerald-900" />
          <p className="font-semibold text-stone-950">{title}</p>
        </div>
        <p className="mt-1 text-sm text-stone-500">{text}</p>
      </div>
    </Link>
  );
}
