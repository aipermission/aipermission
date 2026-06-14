import { Download, RefreshCcw, Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { PaginationBar } from "../components/ui/pagination-bar";
import { TerminalBlock } from "../components/ui/terminal-block";
import { formatBytes } from "../lib/file-transfer-utils";
import { connectorBadgeTone, connectorKindLabel } from "../connectors/templates/common";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { getConnectorModel } from "../connectors/templates/registry";
import { apiDelete, apiDownload, apiGet, apiPost } from "../lib/api";

const statusOptions = [
  { value: "", label: "All statuses" },
  { value: "pending_approval", label: "Pending approval" },
  { value: "pending", label: "Pending" },
  { value: "running", label: "Running" },
  { value: "paused", label: "Paused" },
  { value: "completed", label: "Completed" },
  { value: "canceled", label: "Canceled" },
  { value: "stale", label: "Stale" },
  { value: "failed", label: "Failed" },
  { value: "declined", label: "Declined" },
  { value: "error", label: "Error" },
  { value: "untracked", label: "Not tracked" },
];

const sourceOptions = [
  { value: "", label: "All sources" },
  { value: "mcp", label: "MCP" },
  { value: "manual", label: "Manual" },
  { value: "ui", label: "UI" },
];

export function HistoryPage() {
  const [filters, setFilters] = useState({
    query: "",
    connectorKind: "",
    status: "",
    source: "",
    targetRef: "",
    labelID: "",
  });
  const [state, setState] = useState({
    state: "idle",
    data: [],
    total: 0,
    limit: 50,
    offset: 0,
    next_offset: null,
    error: null,
  });
  const [labels, setLabels] = useState({ state: "idle", data: [], error: null });
  const [targets, setTargets] = useState({ state: "idle", data: [], error: null });
  const [selected, setSelected] = useState(null);

  useEffect(() => {
    void loadLabels();
    void loadHistoryTargets();
  }, []);

  const targetItems = targets.data || [];
  const targetSignature = targetItems.map((target) => target.ref).join(",");
  const connectorKindOptions = useMemo(
    () => [{ value: "", label: "All connectors" }, ...supportedConnectorKinds.map((kind) => ({ value: kind, label: connectorKindLabel(kind) }))],
    []
  );

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadHistory(0);
    }, 250);
    return () => window.clearTimeout(timer);
  }, [filters.query, filters.connectorKind, filters.status, filters.source, filters.targetRef, filters.labelID, targetSignature]);

  useEffect(() => {
    const hasActive = state.data.some((item) => ["pending", "pending_approval", "running", "paused"].includes(item.status));
    if (!hasActive) return undefined;
    const timer = window.setInterval(() => {
      void loadHistory(state.offset, { silent: true });
    }, 1500);
    return () => window.clearInterval(timer);
  }, [state.data, state.offset]);

  const stats = useMemo(() => {
    const data = state.data;
    return {
      total: state.total,
      shown: data.length,
      active: data.filter((item) => ["pending", "pending_approval", "running", "paused"].includes(item.status)).length,
      failed: data.filter((item) => ["failed", "error", "stale"].includes(item.status)).length,
    };
  }, [state.data, state.total]);

  async function loadLabels() {
    setLabels((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/history-labels");
      setLabels({ state: "ready", data: data || [], error: null });
    } catch (error) {
      setLabels({ state: "error", data: [], error: error.message });
    }
  }

  async function loadHistoryTargets() {
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/history/targets");
      setTargets({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  async function loadHistory(offset = state.offset, options = {}) {
    if (!options.silent) {
      setState((current) => ({ ...current, state: "loading", error: null }));
    }
    const params = new URLSearchParams({
      limit: String(state.limit),
      offset: String(Math.max(0, offset)),
    });
    if (filters.query.trim()) params.set("q", filters.query.trim());
    if (filters.connectorKind) params.set("connector_kind", filters.connectorKind);
    if (filters.status) params.set("status", filters.status);
    if (filters.source) params.set("source", filters.source);
    const selectedTarget = targetItems.find((target) => target.ref === filters.targetRef);
    if (selectedTarget?.target_id) {
      params.set("target_id", String(selectedTarget.target_id));
    }
    if (selectedTarget?.profile_id) {
      params.set("profile_id", String(selectedTarget.profile_id));
    }
    if (filters.labelID) params.set("label_id", filters.labelID);
    try {
      const data = await apiGet(`/api/history?${params.toString()}`);
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
      setState((current) => ({ ...current, state: "error", data: [], total: 0, error: error.message }));
    }
  }

  async function openHistoryItem(item) {
    setSelected(item);
    try {
      const detail = await apiGet(`/api/history/${item.id}`);
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
    const nextLabels = await apiPost(`/api/history/${id}/labels`, payload);
    updateItemLabels(id, nextLabels || []);
    await loadLabels();
  }

  async function detachLabel(id, labelID) {
    const nextLabels = await apiDelete(`/api/history/${id}/labels/${labelID}`);
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
          <p className="text-sm text-stone-500">Review every gateway activity through one connector-aware stream.</p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            void loadHistoryTargets();
            void loadHistory(state.offset);
          }}
          disabled={state.state === "loading"}
        >
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-4">
        <HistoryStat label="Total" value={stats.total} />
        <HistoryStat label="Shown" value={stats.shown} />
        <HistoryStat label="Active" value={stats.active} tone="warn" />
        <HistoryStat label="Failed/stale" value={stats.failed} tone="bad" />
      </div>

      <div className="grid gap-3 rounded-lg border border-stone-200 bg-white p-4 lg:grid-cols-[minmax(0,1fr)_150px_160px_150px_220px_220px]">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-400" />
          <Input
            value={filters.query}
            onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))}
            placeholder="Search targets, actions, output, paths, or tokens"
            className="pl-9"
          />
        </div>
        <Select value={filters.connectorKind} onChange={(event) => setFilters((current) => ({ ...current, connectorKind: event.target.value }))}>
          {connectorKindOptions.map((option) => (
            <option key={option.value || "all"} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select value={filters.status} onChange={(event) => setFilters((current) => ({ ...current, status: event.target.value }))}>
          {statusOptions.map((option) => (
            <option key={option.value || "all"} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select value={filters.source} onChange={(event) => setFilters((current) => ({ ...current, source: event.target.value }))}>
          {sourceOptions.map((option) => (
            <option key={option.value || "all"} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select value={filters.targetRef} onChange={(event) => setFilters((current) => ({ ...current, targetRef: event.target.value }))}>
          <option value="">All connectors</option>
          {targetItems.map((target) => (
            <option key={target.ref} value={target.ref}>
              {targetOptionLabel(target)}
            </option>
          ))}
        </Select>
        <Select value={filters.labelID} onChange={(event) => setFilters((current) => ({ ...current, labelID: event.target.value }))}>
          <option value="">All labels</option>
          {labels.data.map((label) => (
            <option key={label.id} value={label.id}>
              {label.name}
            </option>
          ))}
        </Select>
        <Button
          type="button"
          variant="outline"
          onClick={() => setFilters({ query: "", connectorKind: "", status: "", source: "", targetRef: "", labelID: "" })}
        >
          Clear filters
        </Button>
      </div>

      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {labels.state === "error" ? <Notice tone="bad">{labels.error}</Notice> : null}

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[12%] px-4 py-3 font-semibold">Status</th>
              <th className="w-[12%] px-4 py-3 font-semibold">Connector</th>
              <th className="w-[22%] px-4 py-3 font-semibold">Target</th>
              <th className="w-[14%] px-4 py-3 font-semibold">Action</th>
              <th className="w-[20%] px-4 py-3 font-semibold">Summary</th>
              <th className="w-[10%] px-4 py-3 font-semibold">Labels</th>
              <th className="w-[10%] px-4 py-3 text-right font-semibold">Time</th>
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
                  No history yet.
                </td>
              </tr>
            ) : null}
            {state.state !== "loading"
              ? state.data.map((item) => (
                  <tr key={item.id} className="cursor-pointer transition hover:bg-stone-50" onClick={() => openHistoryItem(item)}>
                    <td className="px-4 py-3">
                      <StatusBadge status={item.status} />
                    </td>
                    <td className="px-4 py-3">
                      <ConnectorBadge kind={item.connector_kind} />
                    </td>
                    <td className="truncate px-4 py-3">
                      <div className="truncate font-medium text-stone-900">{item.target_name || "-"}</div>
                      {item.profile_label ? <div className="truncate text-xs text-stone-500">{item.profile_label}</div> : null}
                    </td>
                    <td className="px-4 py-3">
                      <ActionBadge item={item} />
                    </td>
                    <td className="truncate px-4 py-3 text-stone-700">{entrySummary(item)}</td>
                    <td className="px-4 py-3">
                      <LabelPreview labels={item.labels || []} />
                    </td>
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

  const attachedLabels = item.labels || [];
  const attachedNames = new Set(attachedLabels.map((label) => label.name.toLowerCase()));
  const labelQuery = labelName.trim().toLowerCase();
  const suggestions = labels
    .filter((label) => !attachedNames.has(label.name.toLowerCase()))
    .filter((label) => !labelQuery || label.name.toLowerCase().includes(labelQuery))
    .slice(0, 10);
  const showSuggestions = suggestionsOpen && suggestions.length > 0;
  const input = entryInput(item);
  const output = entryOutput(item);

  function focusLabelInput() {
    window.setTimeout(() => labelInputRef.current?.focus(), 0);
  }

  async function addLabel(value = labelName) {
    const name = value.trim();
    if (!name) return;
    if (attachedNames.has(name.toLowerCase())) {
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

  async function downloadTransfer() {
    setState({ state: "downloading", error: null });
    try {
      await apiDownload(`/api/file-transfers/${item.source_ref_id}/download`, transferFileName(item));
      setState({ state: "idle", error: null });
    } catch (error) {
      setState({ state: "error", error: error.message });
    }
  }

  return (
    <Dialog
      open={Boolean(item)}
      title={`History #${item.id}`}
      description={`${item.target_name || "unknown target"} · ${formatDateTime(item.created_at)}`}
      onClose={onClose}
      size="wide"
      className="h-[calc(100vh-100px)] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden p-0"
    >
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]">
        <div className="grid gap-2 border-b border-stone-200 px-5 py-3">
          <div className="grid min-w-0 gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
            <span className="min-w-0 truncate text-sm text-stone-600">{entrySummary(item)}</span>
            <div className="flex min-w-0 flex-wrap items-center gap-2 md:justify-end">
              <StatusBadge status={item.status} />
              <ConnectorBadge kind={item.connector_kind} />
              <ActionBadge item={item} />
              <SourceBadge source={item.source} />
              {item.profile_label ? <Badge>{item.profile_label}</Badge> : null}
              {item.token_name ? <Badge>{item.token_name}</Badge> : null}
              {item.exit_code !== undefined && item.exit_code !== null ? <Badge>exit {item.exit_code}</Badge> : null}
            </div>
          </div>

          {item.status === "untracked" ? (
            <p className="truncate rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-950">
              <span className="font-semibold">Not tracked:</span> output or exit status was not captured for this manual terminal command.
            </p>
          ) : null}
          {item.error ? <Notice tone="bad">{item.error}</Notice> : null}

          <form className="relative min-w-0" onSubmit={(event) => event.preventDefault()}>
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
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
        </div>

        <div className="grid min-h-0 gap-4 p-5 lg:grid-cols-2">
          <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
            <SectionHeader label={inputLabel(item)} value={input} />
            <TerminalBlock>{input || "No input captured."}</TerminalBlock>
          </div>
          <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
            <SectionHeader label="Output" value={output} />
            {item.activity_type === "file_transfer" ? <TransferDetail item={item} /> : <TerminalBlock>{output || "No output captured."}</TerminalBlock>}
          </div>
        </div>

        {item.activity_type === "file_transfer" && item.action_name === "download" && item.status === "completed" ? (
          <div className="flex justify-end border-t border-stone-200 px-5 py-3">
            <Button type="button" onClick={downloadTransfer} disabled={state.state === "downloading"}>
              <Download className="h-4 w-4" />
              Save download
            </Button>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}

function TransferDetail({ item }) {
  const percent = progressPercent(item);
  return (
    <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-3 rounded-md border border-stone-200 bg-stone-50 p-4">
      <div className="grid gap-2 text-sm">
        <TransferField label="Remote path" value={item.summary || item.input_text || "-"} mono />
        <TransferField label="Bytes" value={`${formatBytes(item.bytes_done || 0)} / ${formatBytes(item.bytes_total || 0)}`} />
      </div>
      <div className="grid gap-1">
        <div className="h-2 overflow-hidden rounded-full bg-stone-100">
          <div className="h-full rounded-full bg-emerald-700" style={{ width: `${percent}%` }} />
        </div>
        <div className="flex items-center justify-between text-xs text-stone-500">
          <span>{item.status}</span>
          <span>{percent}%</span>
        </div>
      </div>
      <TerminalBlock>{item.output_text || item.error || "No transfer output captured."}</TerminalBlock>
    </div>
  );
}

function TransferField({ label, value, mono = false }) {
  return (
    <div className="grid min-w-0 gap-1">
      <span className="text-xs font-semibold uppercase text-stone-500">{label}</span>
      <span className={`min-w-0 break-words ${mono ? "font-mono text-xs" : ""}`}>{value || "-"}</span>
    </div>
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
    canceled: "warn",
    paused: "warn",
    pending: "neutral",
    running: "neutral",
    pending_approval: "warn",
    declined: "warn",
    stale: "warn",
    untracked: "warn",
    failed: "bad",
    error: "bad",
  }[status] || "neutral";
  return <Badge tone={tone}>{statusLabel(status)}</Badge>;
}

function ConnectorBadge({ kind }) {
  return <Badge tone={connectorBadgeTone(kind)}>{connectorKindLabel(kind || "connector")}</Badge>;
}

function ActionBadge({ item }) {
  const label = {
    command: item.source === "manual" ? "manual" : item.action_name || "exec",
    action: item.action_name || "action",
    file_transfer: item.action_name || "transfer",
  }[item.activity_type] || item.action_name || "activity";
  return <Badge tone={item.activity_type === "file_transfer" ? "warn" : "neutral"}>{label}</Badge>;
}

function SourceBadge({ source }) {
  const value = source || "mcp";
  const tone = value === "manual" ? "warn" : value === "ui" ? "good" : "neutral";
  return <Badge tone={tone}>{value}</Badge>;
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

function entrySummary(item) {
  return item.summary || item.title || item.action_name || item.input_text || item.error || "-";
}

function entryInput(item) {
  if (item.input_text) return item.input_text;
  return prettyJSON(item.input_json);
}

function entryOutput(item) {
  if (item.output_text) return item.output_text;
  const json = prettyJSON(item.output_json);
  if (json && json !== "{}") return json;
  return item.error || "";
}

function inputLabel(item) {
  if (item.activity_type === "file_transfer") return item.action_name === "upload" ? "Upload" : "Download";
  if (item.activity_type === "action") return `Input: ${item.action_name}`;
  return "Command";
}

function prettyJSON(value) {
  if (!value) return "";
  if (typeof value !== "string") {
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return String(value);
    }
  }
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

function progressPercent(item) {
  const total = Number(item.bytes_total || item.progress_total || 0);
  const done = Number(item.bytes_done || item.progress_current || 0);
  if (total <= 0) return item.status === "completed" ? 100 : 0;
  return Math.max(0, Math.min(100, Math.round((done / total) * 100)));
}

function transferFileName(item) {
  const summary = String(item.summary || "").split("/").filter(Boolean).pop();
  return summary || item.title || "aipermission-download";
}

function targetOptionLabel(target) {
  if (!target) return "Unknown connector";
  const model = getConnectorModel(target.connector_kind);
  const name = model?.targetDisplayName?.({ target }) || target.target_name || target.name || target.ref || "connector";
  const profile = model?.targetProfileLabel?.({ target }) || target.profile_label || "default";
  return `${name} / ${profile}`;
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
