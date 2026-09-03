import { Download, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { connectorBadgeTone, connectorKindLabel } from "../../connectors/templates/common";
import { getConnectorModel } from "../../connectors/templates/registry";
import { apiDownload } from "../../lib/api";
import { formatBytes } from "../../lib/file-transfer-utils";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { CopyButton } from "../ui/copy-button";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";
import { ProgressBar } from "../ui/progress-bar";
import { TerminalBlock } from "../ui/terminal-block";
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
            {item.activity_type === "file_transfer" ? (
              <TransferDetail item={item} />
            ) : (
              <TerminalBlock>{output || "No output captured."}</TerminalBlock>
            )}
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
        <ProgressBar value={percent} />
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
  const tone =
    {
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
  const label =
    {
      command: item.source === "manual" ? "manual" : item.action_name || "exec",
      action: item.action_name || "action",
      file_transfer: item.action_name || "transfer",
    }[item.activity_type] ||
    item.action_name ||
    "activity";
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
  const summary = String(item.summary || "")
    .split("/")
    .filter(Boolean)
    .pop();
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
export {
  ActionBadge,
  ConnectorBadge,
  HistoryDialog,
  HistoryStat,
  LabelPreview,
  SourceBadge,
  StatusBadge,
  entrySummary,
  formatShortTime,
  targetOptionLabel,
};
