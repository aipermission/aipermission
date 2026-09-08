import { RefreshCcw, Search } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { apiGet } from "../lib/api";
import { useRequestGuard } from "../lib/request-guard";
import { useGateway } from "../lib/gateway-context";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { PaginationBar } from "../components/ui/pagination-bar";
import { TerminalBlock } from "../components/ui/terminal-block";

const actorOptions = [
  { value: "", label: "All actors" },
  { value: "user", label: "User" },
  { value: "mcp", label: "MCP" },
];

export function AuditLogsPage() {
  const { targets } = useGateway();
  const [filters, setFilters] = useState({ query: "", projectID: "", actor: "", connectorKind: "", targetID: "" });
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const [state, setState] = useState({
    state: "idle",
    data: [],
    total: 0,
    limit: 50,
    offset: 0,
    next_offset: null,
    error: null,
  });
  const [selected, setSelected] = useState(null);
  const requests = useRequestGuard("audit-logs");
  const loadProjectsForEffect = useEffectEvent(() => loadAuditProjects(requests, setProjects));
  const loadAuditLogsForEffect = useEffectEvent((offset) => loadAuditLogs(offset));

  useEffect(() => {
    void loadProjectsForEffect();
  }, []);

  useEffect(() => {
    requests.invalidate("list");
    const timer = window.setTimeout(() => {
      void loadAuditLogsForEffect(0);
    }, 250);
    return () => window.clearTimeout(timer);
  }, [filters.query, filters.projectID, filters.actor, filters.connectorKind, filters.targetID, requests]);

  const targetOptions = useMemo(() => {
    const options = new Map();
    (targets.data || []).forEach((target) => {
      if (filters.projectID && String(target.project_id) !== String(filters.projectID)) return;
      const id = target.target_id || target.id;
      if (id) options.set(String(id), target.target_name || target.name || target.ref || `target ${id}`);
    });
    state.data.forEach((item) => {
      if (filters.projectID && String(item.project_id) !== String(filters.projectID)) return;
      if (item.target_id) options.set(String(item.target_id), item.target_name || item.target_ref || `target ${item.target_id}`);
      if (!item.target_id && item.runtime_id) {
        options.set(`runtime:${item.runtime_id}`, item.target_name || item.target_ref || `runtime ${item.runtime_id}`);
      }
    });
    return [...options.entries()].sort((left, right) => left[1].localeCompare(right[1]));
  }, [targets.data, state.data, filters.projectID]);

  const connectorKindOptions = useMemo(() => {
    const kinds = new Set();
    (targets.data || []).forEach((target) => {
      if (target.connector_kind) kinds.add(target.connector_kind);
    });
    state.data.forEach((item) => {
      if (item.connector_kind) kinds.add(item.connector_kind);
    });
    return [...kinds].sort();
  }, [targets.data, state.data]);

  const stats = useMemo(
    () => ({
      total: state.total,
      shown: state.data.length,
      mcp: state.data.filter((item) => item.actor_type === "mcp").length,
      user: state.data.filter((item) => item.actor_type === "user").length,
    }),
    [state.data, state.total],
  );

  async function loadAuditLogs(offset = state.offset) {
    const request = requests.begin("list");
    setState((current) => ({ ...current, state: "loading", error: null }));
    const params = new URLSearchParams({
      limit: String(state.limit),
      offset: String(Math.max(0, offset)),
    });
    if (filters.query.trim()) params.set("q", filters.query.trim());
    if (filters.projectID) params.set("project_id", filters.projectID);
    if (filters.actor) params.set("actor", filters.actor);
    if (filters.connectorKind) params.set("connector_kind", filters.connectorKind);
    if (filters.targetID && !filters.targetID.startsWith("runtime:")) params.set("target_id", filters.targetID);
    if (filters.targetID?.startsWith("runtime:")) params.set("runtime_id", filters.targetID.slice("runtime:".length));
    try {
      const data = await apiGet(`/api/audit-logs?${params.toString()}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      setState({
        state: "ready",
        data: data.items || [],
        total: data.total || 0,
        limit: data.limit || state.limit,
        offset: data.offset || 0,
        next_offset: data.next_offset ?? null,
        error: null,
      });
    } catch (error) {
      if (!request.isCurrent()) return;
      setState((current) => ({ ...current, state: "error", data: [], total: 0, error: error.message }));
    } finally {
      request.complete();
    }
  }

  async function openAuditItem(item) {
    const request = requests.begin("detail");
    setSelected(item);
    try {
      const detail = await apiGet(`/api/audit-logs/${item.id}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      setSelected(detail);
    } catch {
      if (!request.isCurrent()) return;
      setSelected(item);
    } finally {
      request.complete();
    }
  }

  const pageStart = state.total === 0 ? 0 : state.offset + 1;
  const pageEnd = Math.min(state.offset + state.data.length, state.total);

  return (
    <section className="mx-auto grid min-w-0 w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Audit Logs</h1>
          <p className="text-sm text-stone-500">Security, token, permission, console, and MCP execution events.</p>
        </div>
        <Button type="button" variant="outline" onClick={() => loadAuditLogs(state.offset)} disabled={state.state === "loading"}>
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-4">
        <AuditStat label="Events" value={stats.total} />
        <AuditStat label="Shown" value={stats.shown} tone="neutral" />
        <AuditStat label="MCP" value={stats.mcp} tone="warn" />
        <AuditStat label="User" value={stats.user} tone="good" />
      </div>

      <AuditFilters
        filters={filters}
        setFilters={setFilters}
        projects={projects.data}
        connectorKinds={connectorKindOptions}
        targets={targetOptions}
      />

      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}

      <div data-testid="audit-table-scroll" className="overflow-x-auto rounded-lg border border-stone-200 bg-white">
        <table className="w-full min-w-[860px] table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[10%] px-4 py-3 font-semibold">Actor</th>
              <th className="w-[24%] px-4 py-3 font-semibold">Action</th>
              <th className="w-[18%] px-4 py-3 font-semibold">Target</th>
              <th className="w-[16%] px-4 py-3 font-semibold">Token</th>
              <th className="w-[22%] px-4 py-3 font-semibold">Payload</th>
              <th className="w-[10%] px-4 py-3 text-right font-semibold">Time</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-100">
            {state.state === "loading" ? (
              <tr>
                <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={6}>
                  Loading audit logs...
                </td>
              </tr>
            ) : null}
            {state.state === "ready" && state.data.length === 0 ? (
              <tr>
                <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={6}>
                  No audit events match these filters.
                </td>
              </tr>
            ) : null}
            {state.state === "ready"
              ? state.data.map((item) => (
                  <tr key={item.id} className="transition hover:bg-stone-50">
                    <td className="px-4 py-3">
                      <ActorBadge actor={item.actor_type} />
                    </td>
                    <td className="truncate px-4 py-3">
                      <button
                        type="button"
                        className="max-w-full rounded outline-none focus-visible:ring-2 focus-visible:ring-emerald-600 focus-visible:ring-offset-2"
                        aria-label={`Open audit details for ${item.action || "unknown action"}`}
                        onClick={() => openAuditItem(item)}
                      >
                        <ActionBadge action={item.action} />
                      </button>
                    </td>
                    <td className="truncate px-4 py-3">
                      <div className="truncate font-medium text-stone-900">{auditTargetLabel(item)}</div>
                      {item.project_name ? <div className="truncate text-xs text-stone-500">{item.project_name}</div> : null}
                    </td>
                    <td className="truncate px-4 py-3 text-stone-600">{item.token_name || "-"}</td>
                    <td className="truncate px-4 py-3 font-mono text-xs text-stone-700">{payloadPreview(item.payload_json)}</td>
                    <td className="px-4 py-3 text-right text-xs text-stone-500">{formatShortTime(item.created_at)}</td>
                  </tr>
                ))
              : null}
          </tbody>
        </table>
      </div>

      <PaginationBar
        start={pageStart}
        end={pageEnd}
        total={state.total}
        disabled={state.state === "loading"}
        onPrevious={() => loadAuditLogs(Math.max(0, state.offset - state.limit))}
        onNext={() => loadAuditLogs(state.next_offset)}
        hasPrevious={state.offset > 0}
        hasNext={state.next_offset !== null && state.next_offset !== undefined}
      />

      <AuditDialog item={selected} onClose={() => setSelected(null)} />
    </section>
  );
}

function AuditFilters({ filters, setFilters, projects, connectorKinds, targets }) {
  const update = (field, value) => setFilters((current) => ({ ...current, [field]: value }));
  return (
    <div className="grid gap-3 rounded-lg border border-stone-200 bg-white p-4 lg:grid-cols-5">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-400" />
        <Input
          aria-label="Search audit logs"
          value={filters.query}
          onChange={(event) => update("query", event.target.value)}
          placeholder="Search actions, names, or payload"
          className="pl-9"
        />
      </div>
      <Select
        aria-label="Filter audit logs by project"
        value={filters.projectID}
        onChange={(event) => setFilters((current) => ({ ...current, projectID: event.target.value, targetID: "" }))}
      >
        <option value="">All projects</option>
        {projects.map((project) => (
          <option key={project.id} value={project.id}>
            {project.name}
          </option>
        ))}
      </Select>
      <Select aria-label="Filter audit logs by actor" value={filters.actor} onChange={(event) => update("actor", event.target.value)}>
        {actorOptions.map((option) => (
          <option key={option.value || "all"} value={option.value}>
            {option.label}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Filter audit logs by connector type"
        value={filters.connectorKind}
        onChange={(event) => update("connectorKind", event.target.value)}
      >
        <option value="">All types</option>
        {connectorKinds.map((kind) => (
          <option key={kind} value={kind}>
            {kind}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Filter audit logs by connector"
        value={filters.targetID}
        onChange={(event) => update("targetID", event.target.value)}
      >
        <option value="">All connectors</option>
        {targets.map(([id, name]) => (
          <option key={id} value={id}>
            {name}
          </option>
        ))}
      </Select>
    </div>
  );
}

async function loadAuditProjects(requests, setProjects) {
  const request = requests.begin("projects");
  try {
    const data = await apiGet("/api/projects", { signal: request.signal });
    if (!request.isCurrent()) return;
    setProjects({ state: "ready", data: data.items || [], error: null });
  } catch (error) {
    if (!request.isCurrent()) return;
    setProjects({ state: "error", data: [], error: error.message });
  } finally {
    request.complete();
  }
}

function AuditStat({ label, value, tone = "neutral" }) {
  return (
    <div className="rounded-lg border border-stone-200 bg-white p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium text-stone-500">{label}</span>
        <Badge tone={tone}>{value}</Badge>
      </div>
    </div>
  );
}

function AuditDialog({ item, onClose }) {
  if (!item) return null;
  const payload = prettyPayload(item.payload_json);

  return (
    <Dialog
      open={Boolean(item)}
      title={`Audit #${item.id}`}
      description={formatDateTime(item.created_at)}
      onClose={onClose}
      size="wide"
      className="h-[calc(100vh-100px)] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden p-0"
    >
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]">
        <div className="grid gap-3 border-b border-stone-200 px-5 py-3">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <ActionBadge action={item.action} />
            <ActorBadge actor={item.actor_type} />
            {item.connector_kind ? <Badge tone="neutral">{item.connector_kind}</Badge> : null}
            {auditTargetLabel(item) !== "-" ? <Badge>{auditTargetLabel(item)}</Badge> : null}
            {item.token_name ? <Badge>{item.token_name}</Badge> : null}
          </div>
        </div>

        <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2 p-5">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-semibold uppercase text-stone-500">Payload</span>
            <CopyButton value={payload} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" />
          </div>
          <TerminalBlock>{payload}</TerminalBlock>
        </div>
      </div>
    </Dialog>
  );
}

function ActorBadge({ actor }) {
  const tone = actor === "user" ? "good" : actor === "mcp" ? "warn" : "neutral";
  return <Badge tone={tone}>{actor || "unknown"}</Badge>;
}

function ActionBadge({ action }) {
  return <Badge tone={actionTone(action)}>{action || "unknown"}</Badge>;
}

function actionTone(action) {
  const value = action || "";
  if (value.includes(".blocked") || value.includes(".error") || value.includes(".failed")) return "bad";
  if (value.includes(".decline") || value.includes(".pending")) return "warn";
  if (value.includes(".completed") || value.includes(".created") || value.includes(".run")) return "good";
  return "neutral";
}

function payloadPreview(value) {
  const parsed = parsePayload(value);
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    const parts = ["request_id", "session_id", "command", "reason", "exit_code", "user_note"]
      .filter((key) => parsed[key] !== undefined && parsed[key] !== null && parsed[key] !== "")
      .map((key) => `${key}: ${oneLine(parsed[key])}`);
    if (parts.length > 0) return parts.join(", ");
  }
  return oneLine(value || "{}");
}

function auditTargetLabel(item) {
  return item.target_name || item.target_ref || (item.target_id ? `target ${item.target_id}` : "-");
}

function prettyPayload(value) {
  const parsed = parsePayload(value);
  if (parsed && typeof parsed === "object") return JSON.stringify(parsed, null, 2);
  return String(parsed || "{}");
}

function parsePayload(value) {
  if (!value) return {};
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

function oneLine(value) {
  return String(value || "")
    .replace(/\s+/g, " ")
    .trim();
}

function formatShortTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function formatDateTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
