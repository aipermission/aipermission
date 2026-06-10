import { useState } from "react";
import { Link } from "react-router-dom";
import { Command, Database, ExternalLink, GitFork, History, Home, KeyRound, ListTree, LockKeyhole, Moon, Package, PlugZap, Power, PowerOff, Settings, Server, Shield, ShieldCheck, Sun, TicketCheck, UploadCloud } from "lucide-react";
import { appVersion, changelogEntries } from "../lib/release";
import { Badge, CountBadge } from "./ui/badge";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { checkForUpdates } from "../lib/update-check";

const navItems = [
  { to: "/", label: "Dashboard", icon: Home },
  { to: "/console", label: "Console", icon: Command },
  { to: "/servers", label: "Servers", icon: Server },
  { to: "/connectors", label: "Connectors", icon: Database },
  { to: "/history", label: "History", icon: History },
  { to: "/audit-logs", label: "Audit Logs", icon: ShieldCheck },
  { to: "/tokens", label: "Tokens", icon: TicketCheck },
  { to: "/ssh-keys", label: "SSH Keys", icon: KeyRound },
  { to: "/mcp-setup", label: "MCP Setup", icon: PlugZap },
  { to: "/security", label: "Security", icon: Shield },
  { to: "/settings", label: "Settings", icon: Settings },
];

const githubUrl = "https://github.com/aipermission/aipermission";
const npmUrl = "https://www.npmjs.com/package/@aipermission/mcp";

export function AppSidebar({ pathname, consoleAttentionCount, activeTransferCount, gatewayState, mcpRuntime, theme, onSetTheme, onSetMCPRuntimeEnabled, onOpenTransferCenter, onSwitchDatabase, onLockDatabase }) {
  const [changelogOpen, setChangelogOpen] = useState(false);
  const [mcpAction, setMCPAction] = useState({ state: "idle", error: null });
  const [updateState, setUpdateState] = useState({ state: "idle", data: null, error: null });
  const mcpStarted = Boolean(mcpRuntime?.data?.enabled);

  async function toggleMCPRuntime() {
    setMCPAction({ state: "saving", error: null });
    try {
      await onSetMCPRuntimeEnabled(!mcpStarted);
      setMCPAction({ state: "idle", error: null });
    } catch (error) {
      setMCPAction({ state: "error", error: error.message });
    }
  }

  async function runUpdateCheck() {
    setUpdateState({ state: "checking", data: null, error: null });
    try {
      const data = await checkForUpdates(appVersion);
      setUpdateState({ state: "ready", data, error: null });
    } catch (error) {
      setUpdateState({ state: "error", data: null, error: error.message || "Update check failed." });
    }
  }

  return (
    <aside className="fixed inset-y-0 left-0 z-20 hidden w-72 overflow-y-auto border-r border-stone-200 bg-white lg:block">
      <div className="flex h-full flex-col">
        <div className="border-b border-stone-200 p-5">
          <div className="flex items-center gap-3">
            <img src="/icon.svg" alt="" className="h-10 w-10 rounded-lg" />
            <div>
              <h1 className="text-base font-semibold">aipermission</h1>
              <p className="text-xs text-stone-500">Local AI gateway</p>
            </div>
          </div>
        </div>

        <nav className="grid gap-1 p-3">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = item.to === pathname;
            const badgeCount = item.to === "/console" ? consoleAttentionCount : 0;
            return (
              <Button key={item.to} asChild variant={active ? "default" : "ghost"} className="justify-start">
                <Link to={item.to}>
                  <Icon className="h-4 w-4" />
                  <span className="min-w-0 flex-1 truncate">{item.label}</span>
                  {badgeCount > 0 ? <CountBadge className="ml-auto">{badgeCount}</CountBadge> : null}
                </Link>
              </Button>
            );
          })}
        </nav>

        <div className="mt-auto border-t border-stone-200 p-4">
          <div className="mb-3 grid grid-cols-2 gap-2">
            <SidebarResourceLink href={githubUrl} icon={GitFork} label="GitHub" />
            <SidebarResourceLink href={npmUrl} icon={Package} label="npmjs" />
          </div>
          <div className="mb-3 grid grid-cols-2 gap-2">
            <ThemeButton active={theme === "dark"} icon={Moon} label="Dark" onClick={() => onSetTheme("dark")} />
            <ThemeButton active={theme === "light"} icon={Sun} label="Light" onClick={() => onSetTheme("light")} />
          </div>
          <button
            type="button"
            className="mb-3 flex h-10 w-full items-center justify-between gap-3 rounded-md border border-stone-300 bg-white px-3 text-sm font-semibold text-stone-800 transition hover:bg-stone-100"
            onClick={() => setChangelogOpen(true)}
          >
            <span className="inline-flex min-w-0 items-center gap-2">
              <ListTree className="h-4 w-4 shrink-0" />
              <span className="truncate">Changelog</span>
            </span>
            <span className="shrink-0 text-xs text-stone-500">{appVersion}</span>
          </button>
          <button
            type="button"
            className="mb-3 flex h-10 w-full items-center justify-between gap-3 rounded-md border border-stone-300 bg-white px-3 text-sm font-semibold text-stone-800 transition hover:bg-stone-100"
            onClick={onOpenTransferCenter}
          >
            <span className="inline-flex min-w-0 items-center gap-2">
              <UploadCloud className="h-4 w-4 shrink-0" />
              <span className="truncate">Transfers</span>
            </span>
            {activeTransferCount > 0 ? <CountBadge>{activeTransferCount}</CountBadge> : <span className="shrink-0 text-xs text-stone-500">idle</span>}
          </button>
          <div className="grid gap-3 rounded-lg border border-stone-200 bg-stone-50 p-3 dark-panel-subtle">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-medium text-stone-500">Gateway</span>
              <Badge tone={gatewayState === "running" ? "good" : gatewayState === "unreachable" ? "bad" : "warn"}>
                {gatewayState}
              </Badge>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-medium text-stone-500">MCP</span>
              <Badge tone={mcpStarted ? "good" : "bad"}>{mcpStarted ? "Started" : "Stopped"}</Badge>
            </div>
            <Button
              type="button"
              variant={mcpStarted ? "outline" : "default"}
              className="h-9 w-full px-3"
              onClick={toggleMCPRuntime}
              disabled={mcpRuntime?.state === "loading" || mcpAction.state === "saving"}
              title={mcpStarted ? "Stop MCP command execution" : "Start MCP command execution"}
            >
              {mcpStarted ? <PowerOff className="h-4 w-4" /> : <Power className="h-4 w-4" />}
              {mcpStarted ? "Stop MCP" : "Start MCP"}
            </Button>
            {mcpAction.error ? <p className="text-xs text-red-600">{mcpAction.error}</p> : null}
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2">
            <Button type="button" variant="outline" className="px-3" onClick={onSwitchDatabase}>
              Switch
            </Button>
            <Button type="button" variant="outline" className="px-3" onClick={onLockDatabase}>
              <LockKeyhole className="h-4 w-4" />
              Lock
            </Button>
          </div>
        </div>
      </div>
      <Dialog
        open={changelogOpen}
        title="Changelog"
        description={`Current app version: ${appVersion}`}
        onClose={() => setChangelogOpen(false)}
        size="lg"
        bodyClassName="max-h-[calc(100vh-180px)] overflow-y-auto"
      >
        <div className="grid gap-5">
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-stone-200 bg-stone-50 p-3">
            <div>
              <p className="text-sm font-semibold text-stone-950">Updates</p>
              <p className="text-xs text-stone-500">Check GitHub Releases manually. No background update checks run.</p>
            </div>
            <Button type="button" variant="outline" onClick={runUpdateCheck} disabled={updateState.state === "checking"}>
              {updateState.state === "checking" ? "Checking..." : "Check for updates"}
            </Button>
            {updateState.state === "ready" ? (
              <p className={`basis-full text-sm ${updateState.data.updateAvailable ? "text-amber-700" : "text-emerald-700"}`}>
                {updateState.data.updateAvailable
                  ? `Update available: ${updateState.data.latestVersion}.`
                  : `You are up to date. Latest release: ${updateState.data.latestVersion}.`}{" "}
                <a className="font-semibold underline" href={updateState.data.releaseUrl} target="_blank" rel="noreferrer">
                  View releases
                </a>
              </p>
            ) : null}
            {updateState.state === "error" ? <p className="basis-full text-sm text-red-700">{updateState.error}</p> : null}
          </div>
          {changelogEntries.map((entry) => (
            <section key={entry.version} className="grid gap-3">
              <div className="flex items-center justify-between gap-3 border-b border-stone-200 pb-2">
                <h3 className="text-sm font-semibold text-stone-950">{entry.version}</h3>
                <Badge>{entry.label}</Badge>
              </div>
              {entry.sections.map((section) => (
                <div key={section.title} className="grid gap-2">
                  <h4 className="text-xs font-semibold uppercase text-stone-500">{section.title}</h4>
                  <ul className="grid gap-2 text-sm text-stone-700">
                    {section.items.map((item) => (
                      <li key={item} className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
                        {item}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </section>
          ))}
        </div>
      </Dialog>
    </aside>
  );
}

function ThemeButton({ active, icon: Icon, label, onClick }) {
  return (
    <button
      type="button"
      className={`inline-flex h-9 items-center justify-center gap-2 rounded-md border px-3 text-sm font-semibold transition ${
        active
          ? "border-emerald-800 bg-emerald-950 text-white permission-button-active"
          : "border-stone-300 bg-white text-stone-800 hover:bg-stone-100"
      }`}
      onClick={onClick}
      aria-pressed={active}
    >
      <Icon className="h-4 w-4" />
      {label}
    </button>
  );
}

function SidebarResourceLink({ href, icon: Icon, label }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-stone-300 bg-white px-3 text-sm font-semibold text-stone-800 transition hover:bg-stone-100"
    >
      <Icon className="h-4 w-4" />
      {label}
      <ExternalLink className="h-3 w-3 text-stone-400" />
    </a>
  );
}
