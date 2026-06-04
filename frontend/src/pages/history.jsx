import { RefreshCcw, Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { apiDelete, apiGet, apiPost } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { PaginationBar } from "../components/ui/pagination-bar";
import { TerminalBlock } from "../components/ui/terminal-block";

const statusOptions = [
  { value: "", label: "All statuses" },
  { value: "pending_approval", label: "Pending" },
  { value: "running", label: "Running" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  { value: "declined", label: "Declined" },
  { value: "error", label: "Error" },
  { value: "untracked", label: "Not tracked" },
];

const sourceOptions = [
  { value: "", label: "All sources" },
  { value: "mcp", label: "MCP" },
  { value: "manual", label: "Manual" },
];

export function HistoryPage() {
  const { servers, approvals, loadApprovals } = useGateway();
  const [filters, setFilters] = useState({ query: "", status: "", source: "", serverID: "", labelID: "" });
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
  const [labels, setLabels] = useState({ state: "idle", data: [], error: null });

  const stats = useMemo(() => {
    const data = state.data;
    return {
      total: state.total,
      shown: data.length,
      pending: data.filter((item) => item.status === "pending_approval").length,
      failed: data.filter((item) => item.status === "failed" || item.status === "error").length,
    };
  }, [state.data, state.total]);

  useEffect(() => {
    void loadLabels();
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadHistory(0);
    }, 250);
    return () => window.clearTimeout(timer);
  }, [filters.query, filters.status, filters.source, filters.serverID, filters.labelID]);

  async function loadLabels() {
    setLabels((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/history-labels");
      setLabels({ state: "ready", data: data || [], error: null });
    } catch (error) {
      setLabels({ state: "error", data: [], error: error.message });
    }
  }

  async function loadHistory(offset = state.offset) {
    setState((current) => ({ ...current, state: "loading", error: null }));
    const params = new URLSearchParams({
      paginated: "true",
      limit: String(state.limit),
      offset: String(Math.max(0, offset)),
    });
    if (filters.query.trim()) params.set("q", filters.query.trim());
    if (filters.status) params.set("status", filters.status);
    if (filters.source) params.set("source", filters.source);
    if (filters.serverID) params.set("server_id", filters.serverID);
    if (filters.labelID) params.set("label_id", filters.labelID);
    try {
      const data = await apiGet(`/api/approvals?${params.toString()}`);
      setState({
        state: "ready",
        data: data.items || [],
        total: data.total || 0,
        limit: data.limit || state.limit,
        offset: data.offset || 0,
        next_offset: data.next_offset ?? null,
        error: null,
      });
      await loadApprovals();
    } catch (error) {
      setState((current) => ({ ...current, state: "error", data: [], total: 0, error: error.message }));
    }
  }

  async function openHistoryItem(item) {
    setSelected(item);
    try {
      const detail = await apiGet(`/api/approvals/${item.id}`);
      setSelected(detail);
    } catch {
      setSelected(item);
    }
  }

  function updateItemLabels(id, nextLabels) {
    setSelected((current) => (current?.id === id ? { ...current, labels: nextLabels } : current));
    setState((current) => ({
      ...current,
      data: current.data.map((item) => (item.id === id ? { ...item, labels: nextLabels } : item)),
    }));
  }

  async function attachLabel(id, payload) {
    const nextLabels = await apiPost(`/api/approvals/${id}/labels`, payload);
    updateItemLabels(id, nextLabels || []);
    await loadLabels();
  }

  async function detachLabel(id, labelID) {
    const nextLabels = await apiDelete(`/api/approvals/${id}/labels/${labelID}`);
    updateItemLabels(id, nextLabels || []);
    if (filters.labelID && String(labelID) === String(filters.labelID)) {
      setState((current) => ({
        ...current,
        data: current.data.filter((item) => item.id !== id),
        total: Math.max(0, current.total - 1),
      }));
    }
  }

  const pageStart = state.total === 0 ? 0 : state.offset + 1;
  const pageEnd = Math.min(state.offset + state.data.length, state.total);

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">History</h3>
          <p className="text-sm text-stone-500">Review MCP and approval command requests executed through the gateway.</p>
        </div>
        <Button type="button" variant="outline" onClick={() => loadHistory(state.offset)} disabled={state.state === "loading"}>
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-4">
        <HistoryStat label="Total" value={stats.total} />
        <HistoryStat label="Shown" value={stats.shown} />
        <HistoryStat label="Pending" value={stats.pending} tone="warn" />
        <HistoryStat label="Failed/error" value={stats.failed} tone="bad" />
      </div>

      <div className="grid gap-3 rounded-lg border border-stone-200 bg-white p-4 md:grid-cols-[minmax(0,1fr)_150px_180px_180px_180px]">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-400" />
          <Input
            value={filters.query}
            onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))}
            placeholder="Search commands, reasons, output, or tokens"
            className="pl-9"
          />
        </div>
        <Select
          value={filters.source}
          onChange={(event) => setFilters((current) => ({ ...current, source: event.target.value }))}
        >
          {sourceOptions.map((option) => (
            <option key={option.value || "all"} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select
          value={filters.serverID}
          onChange={(event) => setFilters((current) => ({ ...current, serverID: event.target.value }))}
        >
          <option value="">All servers</option>
          {servers.data.map((server) => (
            <option key={server.id} value={server.id}>
              {server.name}
            </option>
          ))}
        </Select>
        <Select
          value={filters.labelID}
          onChange={(event) => setFilters((current) => ({ ...current, labelID: event.target.value }))}
        >
          <option value="">All labels</option>
          {labels.data.map((label) => (
            <option key={label.id} value={label.id}>
              {label.name}
            </option>
          ))}
        </Select>
        <Select
          value={filters.status}
          onChange={(event) => setFilters((current) => ({ ...current, status: event.target.value }))}
        >
          {statusOptions.map((option) => (
            <option key={option.value || "all"} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
      </div>

      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {approvals.state === "error" ? <Notice tone="bad">{approvals.error}</Notice> : null}
      {labels.state === "error" ? <Notice tone="bad">{labels.error}</Notice> : null}

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[12%] px-4 py-3 font-semibold">Status</th>
              <th className="w-[16%] px-4 py-3 font-semibold">Server</th>
              <th className="w-[18%] px-4 py-3 font-semibold">Source</th>
              <th className="w-[26%] px-4 py-3 font-semibold">Command</th>
              <th className="w-[8%] px-4 py-3 font-semibold">Labels</th>
              <th className="w-[12%] px-4 py-3 font-semibold">Exit</th>
              <th className="w-[8%] px-4 py-3 text-right font-semibold">Time</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-100">
            {state.state === "loading" ? (
              <tr>
                <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={7}>
                  Loading history...
                </td>
              </tr>
            ) : null}
            {state.state !== "loading" && state.data.length === 0 ? (
              <tr>
                <td className="px-4 py-8 text-center text-sm text-stone-500" colSpan={7}>
                  No command history yet.
                </td>
              </tr>
            ) : null}
            {state.state !== "loading"
              ? state.data.map((item) => (
                  <tr
                    key={item.id}
                    className="cursor-pointer transition hover:bg-stone-50"
                    onClick={() => openHistoryItem(item)}
                  >
                    <td className="px-4 py-3">
                      <StatusBadge status={item.status} />
                    </td>
                    <td className="truncate px-4 py-3 font-medium text-stone-900">{item.server_name}</td>
                    <td className="px-4 py-3">
                      <SourceCell item={item} />
                    </td>
                    <td className="truncate px-4 py-3 font-mono text-xs text-stone-700">{oneLine(item.command)}</td>
                    <td className="px-4 py-3">
                      <LabelPreview labels={item.labels || []} />
                    </td>
                    <td className="px-4 py-3 text-stone-600">{item.exit_code ?? "-"}</td>
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
        onPrevious={() => loadHistory(Math.max(0, state.offset - state.limit))}
        onNext={() => loadHistory(state.next_offset)}
        hasPrevious={state.offset > 0}
        hasNext={state.next_offset !== null && state.next_offset !== undefined}
      />

      <HistoryDialog
        item={selected}
        labels={labels.data}
        onClose={() => setSelected(null)}
        onAttachLabel={attachLabel}
        onDetachLabel={detachLabel}
      />
    </section>
  );
}

function HistoryStat({ label, value, tone = "neutral" }) {
  return (
    <div className="rounded-lg border border-stone-200 bg-white p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium text-stone-500">{label}</span>
        <Badge tone={tone}>{value}</Badge>
      </div>
    </div>
  );
}

function HistoryDialog({ item, labels = [], onClose, onAttachLabel, onDetachLabel }) {
  const [labelName, setLabelName] = useState("");
  const [state, setState] = useState({ state: "idle", error: null });
  const [suggestionsOpen, setSuggestionsOpen] = useState(false);
  const [activeSuggestion, setActiveSuggestion] = useState(0);
  const labelInputRef = useRef(null);

  useEffect(() => {
    setLabelName("");
    setState({ state: "idle", error: null });
    setSuggestionsOpen(false);
    setActiveSuggestion(0);
  }, [item?.id]);

  if (!item) return null;
  const output = [item.stdout, item.stderr, item.error].filter(Boolean).join("\n\n");
  const attachedLabels = item.labels || [];
  const attachedNames = new Set(attachedLabels.map((label) => label.name.toLowerCase()));
  const labelQuery = labelName.trim().toLowerCase();
  const suggestions = labels
    .filter((label) => !attachedNames.has(label.name.toLowerCase()))
    .filter((label) => !labelQuery || label.name.toLowerCase().includes(labelQuery))
    .slice(0, 10);
  const showSuggestions = suggestionsOpen && suggestions.length > 0;

  function focusLabelInput() {
    window.setTimeout(() => labelInputRef.current?.focus(), 0);
  }

  async function addLabel(value = labelName) {
    const name = value.trim();
    if (!name) return;
    const normalized = name.toLowerCase();
    if (attachedNames.has(normalized)) {
      setLabelName("");
      return;
    }
    setState({ state: "saving", error: null });
    try {
      await onAttachLabel(item.id, { name });
      setLabelName("");
      setSuggestionsOpen(false);
      setActiveSuggestion(0);
      setState({ state: "idle", error: null });
      focusLabelInput();
    } catch (error) {
      setState({ state: "error", error: error.message });
      focusLabelInput();
    }
  }

  function handleLabelKeyDown(event) {
    if (event.key === "ArrowDown" && suggestions.length > 0) {
      event.preventDefault();
      setSuggestionsOpen(true);
      setActiveSuggestion((current) => Math.min(current + 1, suggestions.length - 1));
      return;
    }
    if (event.key === "ArrowUp" && suggestions.length > 0) {
      event.preventDefault();
      setSuggestionsOpen(true);
      setActiveSuggestion((current) => Math.max(current - 1, 0));
      return;
    }
    if (event.key === "Escape") {
      setSuggestionsOpen(false);
      return;
    }
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      void addLabel(showSuggestions ? suggestions[activeSuggestion]?.name : labelName);
    }
  }

  function submitLabel(event) {
    event.preventDefault();
    void addLabel();
  }

  async function removeLabel(labelID) {
    setState({ state: "saving", error: null });
    try {
      await onDetachLabel(item.id, labelID);
      setState({ state: "idle", error: null });
      focusLabelInput();
    } catch (error) {
      setState({ state: "error", error: error.message });
      focusLabelInput();
    }
  }

  return (
    <Dialog
      open={Boolean(item)}
      title={`Request #${item.id}`}
      description={`${item.server_name} · ${formatDateTime(item.created_at)}`}
      onClose={onClose}
      size="wide"
      className="h-[calc(100vh-100px)] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden p-0"
    >
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]">
        <div className="grid gap-2 border-b border-stone-200 px-5 py-3">
          <div className="grid min-w-0 gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
            <span className="min-w-0 truncate text-sm text-stone-600">
              {item.reason ? item.reason : "No reason provided."}
            </span>
            <div className="flex min-w-0 flex-wrap items-center gap-2 md:justify-end">
              <StatusBadge status={item.status} />
              {item.token_name ? <Badge>{item.token_name}</Badge> : null}
              <SourceBadge source={item.source} />
              {item.session_id ? <Badge>session {item.session_id}</Badge> : null}
              {item.exit_code !== undefined && item.exit_code !== null ? <Badge>exit {item.exit_code}</Badge> : null}
              {item.output_truncated ? <Badge tone="warn">output truncated</Badge> : null}
            </div>
          </div>
          {item.status === "untracked" ? (
            <p className="truncate rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-950">
              <span className="font-semibold">Not tracked:</span> {trackingReasonLabel(item.tracking_reason)}
            </p>
          ) : null}
          {item.user_note ? (
            <p className="truncate rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-950">
              <span className="font-semibold">User note:</span> {item.user_note}
            </p>
          ) : null}
          <div className="grid gap-2">
            <form className="relative min-w-0" onSubmit={submitLabel}>
              <div className="flex min-h-10 min-w-0 flex-nowrap items-center gap-2 overflow-x-auto rounded-md border border-stone-200 bg-white px-2 py-1.5 focus-within:border-emerald-600 focus-within:ring-2 focus-within:ring-emerald-600/15">
                {attachedLabels.map((label) => (
                  <button
                    key={label.id}
                    type="button"
                    className="inline-flex max-w-44 shrink-0 items-center gap-1 rounded-full border bg-transparent px-2.5 py-1 text-xs font-semibold"
                    style={labelStyle(label)}
                    onClick={() => removeLabel(label.id)}
                    disabled={state.state === "saving"}
                    title="Remove label"
                  >
                    <span className="truncate">{label.name}</span>
                    <X className="h-3 w-3" />
                  </button>
                ))}
                <input
                  ref={labelInputRef}
                  value={labelName}
                  onChange={(event) => {
                    setLabelName(event.target.value);
                    setSuggestionsOpen(true);
                    setActiveSuggestion(0);
                  }}
                  onFocus={() => {
                    setSuggestionsOpen(true);
                    setActiveSuggestion(0);
                  }}
                  onBlur={() => window.setTimeout(() => setSuggestionsOpen(false), 120)}
                  onKeyDown={handleLabelKeyDown}
                  placeholder={attachedLabels.length === 0 ? "Type a label and press Enter" : "Add another label"}
                  disabled={state.state === "saving"}
                  className="h-7 min-w-40 flex-1 shrink-0 border-0 bg-transparent px-1 text-sm outline-none placeholder:text-stone-400"
                />
              </div>
              {showSuggestions ? (
                <div className="absolute left-0 right-0 top-full z-20 mt-1 max-h-56 w-full overflow-auto rounded-md border border-stone-200 bg-white shadow-lg">
                  {suggestions.map((label, index) => (
                    <button
                      key={label.id}
                      type="button"
                      className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm ${
                        index === activeSuggestion ? "bg-emerald-50 text-emerald-950" : "text-stone-800 hover:bg-stone-50"
                      }`}
                      onMouseDown={(event) => {
                        event.preventDefault();
                        void addLabel(label.name);
                      }}
                    >
                      <span className="truncate">{label.name}</span>
                      <span className="text-xs text-stone-400">Enter</span>
                    </button>
                  ))}
                </div>
              ) : null}
            </form>
          </div>
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        </div>

        <div className="grid min-h-0 gap-4 p-5 lg:grid-cols-2">
          <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
            <SectionHeader label="Command" value={item.command} />
            <TerminalBlock>{item.command}</TerminalBlock>
          </div>
          <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
            <SectionHeader label="Output" value={output} />
            <TerminalBlock>{output || "No output captured."}</TerminalBlock>
          </div>
        </div>
      </div>
    </Dialog>
  );
}

function LabelPreview({ labels }) {
  if (!labels.length) {
    return <span className="text-xs text-stone-400">-</span>;
  }
  return (
    <div className="flex min-w-0 flex-wrap gap-1">
      {labels.slice(0, 2).map((label) => (
        <Badge key={label.id} className="max-w-24 truncate bg-transparent" style={labelStyle(label)}>
          {label.name}
        </Badge>
      ))}
      {labels.length > 2 ? <Badge>+{labels.length - 2}</Badge> : null}
    </div>
  );
}

function SourceCell({ item }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <SourceBadge source={item.source} />
      {item.source === "manual" ? (
        <span className="truncate text-xs text-stone-500">local console</span>
      ) : (
        <span className="truncate text-xs text-stone-600">{item.token_name || "deleted token"}</span>
      )}
    </div>
  );
}

function SourceBadge({ source }) {
  const value = source || "mcp";
  return <Badge tone={value === "manual" ? "warn" : "neutral"}>{value === "manual" ? "manual" : "mcp"}</Badge>;
}

function SectionHeader({ label, value }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-xs font-semibold uppercase text-stone-500">{label}</span>
      <CopyButton value={value || ""} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" />
    </div>
  );
}

function StatusBadge({ status }) {
  const tone = {
    completed: "good",
    running: "neutral",
    pending_approval: "warn",
    declined: "warn",
    untracked: "warn",
    failed: "bad",
    error: "bad",
  }[status] || "neutral";
  return <Badge tone={tone}>{statusLabel(status)}</Badge>;
}

function labelStyle(label) {
  const color = label?.color || "#0f766e";
  return {
    borderColor: color,
    color,
  };
}

function statusLabel(status) {
  if (status === "pending_approval") return "pending";
  if (status === "untracked") return "not tracked";
  return status || "unknown";
}

function trackingReasonLabel(reason) {
  const labels = {
    interactive_editor: "interactive editor command",
    interactive_repl: "interactive REPL command",
    interactive_tui: "interactive terminal program",
    nested_shell: "nested shell boundary",
    multiline_or_heredoc: "multiline command preview only",
    compound_command: "compound command shape",
    unsupported_shell: "unsupported shell",
    untrusted_command_text: "command text was not trusted",
    marker_desync: "capture marker desynchronized",
    active_exec_paused: "capture paused while an MCP command was running",
    output_buffer_limit: "output exceeded the capture limit",
    privacy_history_suppressed: "history privacy settings suppressed capture",
  };
  return labels[reason] || reason || "AIPermission did not capture output or lifecycle for this command.";
}

function oneLine(value) {
  return String(value || "").replace(/\s+/g, " ").trim();
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
