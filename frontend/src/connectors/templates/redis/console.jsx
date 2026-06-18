import { Database, Plus, RefreshCcw, Save, Search, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Checkbox, Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";

const defaultPattern = "*";
const defaultLimit = 100;

export function RedisConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const [pattern, setPattern] = useState(defaultPattern);
  const [cursor, setCursor] = useState("0");
  const [keys, setKeys] = useState([]);
  const [selectedKeys, setSelectedKeys] = useState([]);
  const [activeKey, setActiveKey] = useState("");
  const [keyResult, setKeyResult] = useState(null);
  const [valueDraft, setValueDraft] = useState("");
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [ttlDraft, setTTLDraft] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const [resultMode, setResultMode] = useState("value");
  const [confirmDialog, setConfirmDialog] = useState({ open: false, type: "", title: "", description: "", details: [], tone: "warn", pending: false, onConfirm: null });
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";
  const subtlePanelClass = theme === "light" ? "bg-stone-50" : "bg-[#252526]";
  const inputClass = theme === "light" ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400" : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500";
  const rowHoverClass = theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60";
  const activeRowClass = theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const activeItems = useMemo(() => (approvals?.data || []).filter((item) => item.target_ref === target.ref), [approvals?.data, target.ref]);
  const latestAction = activeItems[0] || null;
  const selectedCount = selectedKeys.length;

  useEffect(() => {
    setCursor("0");
    setKeys([]);
    setSelectedKeys([]);
    setActiveKey("");
    setKeyResult(null);
    setValueDraft("");
    setNewKey("");
    setNewValue("");
    setTTLDraft("");
    setResultMode("value");
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    if (!activeSession.active) return;
    void scanKeys({ reset: true });
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  async function runRedisAction({ actionName, input, reason, busy = "running" }) {
    setState({ state: busy, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      setState({ state: "idle", error: "", message: item.display_text || "" });
      await onRefreshActivity?.();
      return item;
    } catch (error) {
      setState({ state: "error", error: error.message || "Redis action failed.", message: "" });
      throw error;
    }
  }

  async function scanKeys({ reset = false } = {}) {
    if (!activeSession.active) return;
    const startCursor = reset ? "0" : cursor || "0";
    const effectivePattern = redisScanPattern(pattern);
    const item = await runRedisAction({
      actionName: "scan_keys",
      input: { pattern: effectivePattern, cursor: startCursor, limit: defaultLimit },
      reason: "manual Redis browser key scan",
      busy: "scanning",
    });
    const output = item.output || {};
    const nextKeys = Array.isArray(output.keys) ? output.keys : [];
    setCursor(String(output.next_cursor || "0"));
    setKeys((current) => uniqueStrings(reset ? nextKeys : [...current, ...nextKeys]));
  }

  function startNewKey() {
    setActiveKey("");
    setKeyResult(null);
    setValueDraft("");
    setNewKey("");
    setNewValue("");
    setTTLDraft("");
    setResultMode("value");
  }

  async function loadKey(key) {
    if (!activeSession.active || !key) return;
    setActiveKey(key);
    setResultMode("value");
    const item = await runRedisAction({
      actionName: "get_key",
      input: { key, limit: 250, max_bytes: 262144 },
      reason: "manual Redis browser key read",
      busy: "reading",
    });
    const output = item.output || {};
    setKeyResult(output);
    setValueDraft(valueToEditableText(output));
    setTTLDraft(output.ttl_ms && output.ttl_ms > 0 ? String(Math.ceil(output.ttl_ms / 1000)) : "");
  }

  async function saveStringValue(event) {
    event?.preventDefault?.();
    const key = activeKey || newKey.trim();
    if (!key) return;
    const value = activeKey ? valueDraft : newValue;
    const ttlSeconds = Number(ttlDraft) > 0 ? Number(ttlDraft) : 0;
    openConfirmDialog({
      type: "save-string",
      title: activeKey ? "Save Redis string" : "Create Redis string key",
      description: activeKey ? "This will overwrite the selected key as a Redis string." : "This will create a Redis string key.",
      tone: "warn",
      details: [
        { label: "Key", value: key },
        { label: "TTL", value: ttlSeconds > 0 ? `${ttlSeconds}s` : "persistent" },
      ],
      onConfirm: async () => {
        await runRedisAction({
          actionName: "set_string",
          input: { key, value, ttl_seconds: ttlSeconds },
          reason: "manual Redis browser string write",
          busy: "writing",
        });
        setNewKey("");
        setNewValue("");
        setKeys((current) => uniqueStrings([...current, key]).sort());
        await loadKey(key);
      },
    });
  }

  async function updateTTL() {
    if (!activeKey) return;
    const ttlSeconds = ttlDraft.trim() === "" ? -1 : Number(ttlDraft);
    const normalizedTTL = Number.isFinite(ttlSeconds) ? ttlSeconds : -1;
    openConfirmDialog({
      type: "ttl",
      title: normalizedTTL < 0 ? "Persist Redis key" : "Update Redis TTL",
      description: normalizedTTL < 0 ? "This removes the expiration from the selected key." : "This changes when the selected key expires.",
      tone: "warn",
      details: [
        { label: "Key", value: activeKey },
        { label: "TTL", value: normalizedTTL < 0 ? "persistent" : `${normalizedTTL}s` },
      ],
      onConfirm: async () => {
        await runRedisAction({
          actionName: "expire_key",
          input: { key: activeKey, ttl_seconds: normalizedTTL },
          reason: "manual Redis browser TTL update",
          busy: "writing",
        });
        await loadKey(activeKey);
      },
    });
  }

  async function deleteSelected() {
    const keysToDelete = selectedKeys.length > 0 ? selectedKeys : activeKey ? [activeKey] : [];
    if (keysToDelete.length === 0) return;
    openConfirmDialog({
      type: "delete",
      title: `Delete ${keysToDelete.length} Redis key${keysToDelete.length === 1 ? "" : "s"}`,
      description: "This permanently deletes the selected Redis key data.",
      tone: "bad",
      details: keysToDelete.slice(0, 8).map((key) => ({ label: "Key", value: key })).concat(keysToDelete.length > 8 ? [{ label: "More", value: `${keysToDelete.length - 8} additional key(s)` }] : []),
      onConfirm: async () => {
        await runRedisAction({
          actionName: "delete_keys",
          input: { keys: keysToDelete },
          reason: "manual Redis browser key delete",
          busy: "deleting",
        });
        setKeys((current) => current.filter((key) => !keysToDelete.includes(key)));
        setSelectedKeys([]);
        if (keysToDelete.includes(activeKey)) {
          setActiveKey("");
          setKeyResult(null);
          setValueDraft("");
        }
      },
    });
  }

  function openConfirmDialog({ type, title, description, details, tone, onConfirm }) {
    setConfirmDialog({ open: true, type, title, description, details, tone, pending: false, onConfirm });
  }

  async function confirmPendingAction() {
    if (!confirmDialog.onConfirm) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    try {
      await confirmDialog.onConfirm();
      setConfirmDialog({ open: false, type: "", title: "", description: "", details: [], tone: "warn", pending: false, onConfirm: null });
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  function toggleSelection(key) {
    setSelectedKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]));
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <div className="grid place-items-center p-8 text-center">
          <div className="grid max-w-lg gap-4">
            <Database className={`mx-auto h-10 w-10 ${mutedClass}`} />
            <div>
              <h3 className="text-lg font-semibold">No active Redis session</h3>
              <p className={`mt-2 text-sm ${mutedClass}`}>Start a structured session to browse Redis keys through the connector approval, history, and audit pipeline.</p>
            </div>
            <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
              Start Redis session
            </Button>
          </div>
        </div>
        <RedisEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  const creatingKey = !activeKey;
  const editableString = creatingKey || keyResult?.type === "string";
  const canSaveString = creatingKey ? Boolean(newKey.trim()) : keyResult?.type === "string";
  const canUpdateTTL = Boolean(activeKey && keyResult && keyResult.type !== "none") && state.state === "idle";

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[340px_minmax(0,1fr)]">
        <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}>
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">Keys</p>
                <p className={`text-xs ${mutedClass}`}>{keys.length} loaded</p>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                {latestAction ? <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>{latestAction.action_name}</Badge> : null}
                <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Refresh keys" onClick={() => scanKeys({ reset: true })} disabled={state.state !== "idle"}>
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
                <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={startNewKey}>
                  <Plus className="h-3.5 w-3.5" />
                  New
                </Button>
                <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={() => setSelectedKeys(selectedKeys.length === keys.length ? [] : keys)}>
                  {selectedKeys.length === keys.length && keys.length > 0 ? "None" : "All"}
                </Button>
              </div>
            </div>
          </div>
          <form className={`grid gap-2 border-b p-3 ${borderClass}`} onSubmit={(event) => { event.preventDefault(); void scanKeys({ reset: true }); }}>
            <div className="relative">
              <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
              <Input className={`pl-9 ${inputClass}`} value={pattern} onChange={(event) => setPattern(event.target.value)} placeholder="SCAN pattern, e.g. user:*" />
            </div>
            <Button type="submit" variant="outline" className="h-9" disabled={state.state !== "idle"}>
              {state.state === "scanning" ? "Scanning" : "Scan keys"}
            </Button>
          </form>
          <div className="min-h-0 overflow-auto p-2">
            {keys.map((key) => (
              <button
                key={key}
                type="button"
                className={`mb-1 grid w-full grid-cols-[auto_minmax(0,1fr)] items-center gap-2 rounded-md border px-2 py-2 text-left text-sm transition ${activeKey === key ? activeRowClass : `${borderClass} ${rowHoverClass}`}`}
                onClick={() => loadKey(key)}
              >
                <Checkbox
                  checked={selectedKeys.includes(key)}
                  onClick={(event) => event.stopPropagation()}
                  onChange={() => toggleSelection(key)}
                  aria-label={`Select ${key}`}
                />
                <span className="truncate font-mono text-xs" title={key}>{key}</span>
              </button>
            ))}
            {keys.length === 0 ? <Notice>{state.state === "scanning" ? "Scanning Redis keys..." : "No keys loaded. Scan to browse this database."}</Notice> : null}
          </div>
          <div className={`flex items-center justify-between gap-2 border-t p-3 ${borderClass}`}>
            <Button type="button" variant="outline" className="h-8 px-3 text-xs" disabled={cursor === "0" || state.state !== "idle"} onClick={() => scanKeys({ reset: false })}>
              More
            </Button>
            <Button type="button" variant="outline" className="h-8 px-3 text-xs text-red-600" disabled={(selectedCount === 0 && !activeKey) || state.state !== "idle"} onClick={deleteSelected}>
              <Trash2 className="h-3.5 w-3.5" />
              Delete {selectedCount || (activeKey ? 1 : "")}
            </Button>
          </div>
        </section>

        <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${borderClass} ${subtlePanelClass}`}>
            <div className="min-w-0">
              <p className="text-sm font-semibold">{activeKey || "New string key"}</p>
              <p className={`truncate text-xs ${mutedClass}`}>{keyResult ? keyMetaText(keyResult) : creatingKey ? "Create a Redis string value." : "Select a key from the browser."}</p>
            </div>
            <div className="flex items-center gap-2">
              {keyResult?.type ? <Badge tone={keyResult.type === "none" ? "neutral" : "good"}>{keyResult.type}</Badge> : null}
              {keyResult ? <CopyButton value={JSON.stringify(keyResult, null, 2)} variant="outline" className="h-8 px-2 text-xs" title="Copy key JSON">JSON</CopyButton> : null}
            </div>
          </div>
          <div className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${borderClass}`}>
            <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
              <Button type="button" variant={resultMode === "value" ? "default" : "outline"} className="h-8 px-3 text-xs" onClick={() => setResultMode("value")}>Value</Button>
              <Button type="button" variant={resultMode === "json" ? "default" : "outline"} className="h-8 px-3 text-xs" onClick={() => setResultMode("json")}>Raw JSON</Button>
              {activeKey ? <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Reload key" onClick={() => loadKey(activeKey)} disabled={state.state !== "idle"}><RefreshCcw className="h-3.5 w-3.5" /></Button> : null}
              {creatingKey ? (
                <Input className={`h-8 min-w-56 flex-1 ${inputClass}`} value={newKey} onChange={(event) => setNewKey(event.target.value)} placeholder="New key name" />
              ) : null}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2">
              <div className="flex items-center gap-1">
                <Input
                  className={`h-8 w-28 ${inputClass}`}
                  value={ttlDraft}
                  onChange={(event) => setTTLDraft(event.target.value)}
                  placeholder="TTL"
                  aria-label="TTL seconds"
                />
                <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Save TTL" disabled={!canUpdateTTL} onClick={updateTTL}>
                  <Save className="h-3.5 w-3.5" />
                </Button>
              </div>
              <Button type="button" className="h-8 px-3 text-xs" disabled={state.state !== "idle" || !canSaveString} onClick={saveStringValue} title={editableString ? "Save Redis string value" : "This Redis type is read-only in the MVP"}>
                {activeKey ? <Save className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                {activeKey ? (editableString ? "Save string" : "Read only") : "Create key"}
              </Button>
            </div>
          </div>
          <div className="min-h-0 overflow-hidden p-4">
            {resultMode === "json" ? (
              <TerminalBlock surface="log" className="h-full min-h-0 text-xs">{keyResult ? JSON.stringify(keyResult, null, 2) : creatingKey ? "New key is not saved yet." : "No key selected."}</TerminalBlock>
            ) : creatingKey ? (
              <Textarea className={`h-full min-h-0 resize-none font-mono text-xs ${inputClass}`} value={newValue} onChange={(event) => setNewValue(event.target.value)} placeholder="String value" />
            ) : (
              <ValuePanel keyResult={keyResult} valueDraft={valueDraft} onValueDraft={setValueDraft} inputClass={inputClass} />
            )}
          </div>
          <div className={`grid gap-2 border-t p-3 ${borderClass}`}>
            {keyResult && keyResult.type !== "string" && resultMode === "value" ? <Notice tone="warn">This Redis type is read-only in the MVP. TTL changes are still available from the toolbar.</Notice> : null}
            {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
            {state.message ? <Notice tone="good">{state.message}</Notice> : null}
          </div>
        </section>
      </div>

      <RedisEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      <RedisConfirmDialog value={confirmDialog} theme={theme} onClose={() => setConfirmDialog({ open: false, type: "", title: "", description: "", details: [], tone: "warn", pending: false, onConfirm: null })} onConfirm={confirmPendingAction} />
    </div>
  );
}

function RedisConfirmDialog({ value, theme, onClose, onConfirm }) {
  const danger = value.tone === "bad";
  const noticeTone = danger ? "bad" : "warn";
  const detailClass = theme === "light" ? "bg-stone-50" : "bg-stone-900/70 text-stone-100";
  return (
    <Dialog
      open={value.open}
      title={value.title}
      description={value.description}
      onClose={onClose}
      closeDisabled={value.pending}
      size="md"
      closeOnOverlay={!value.pending}
      closeOnEscape={!value.pending}
      className={theme === "light" ? "" : "border-stone-700 bg-[#252526] text-stone-100"}
      bodyClassName={theme === "light" ? "" : "bg-[#252526]"}
    >
      <div className="grid gap-4">
        <Notice tone={noticeTone}>{danger ? "This operation cannot be undone." : "Review the Redis write before continuing."}</Notice>
        {value.details?.length ? (
          <div className={`max-h-56 overflow-auto rounded-md border border-stone-300 p-3 text-sm ${detailClass}`}>
            {value.details.map((item, index) => (
              <div key={`${item.label}:${index}`} className="grid gap-1 py-1 sm:grid-cols-[110px_minmax(0,1fr)]">
                <span className="text-xs font-semibold uppercase text-stone-500">{item.label}</span>
                <span className="break-all font-mono text-xs">{item.value}</span>
              </div>
            ))}
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
            Cancel
          </Button>
          <Button type="button" variant={danger ? "danger" : "default"} onClick={onConfirm} disabled={value.pending}>
            {value.pending ? "Working..." : danger ? "Delete" : "Confirm"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function ValuePanel({ keyResult, valueDraft, onValueDraft, inputClass }) {
  if (!keyResult) {
    return <Notice>Select a key from the left panel to inspect its value.</Notice>;
  }
  if (keyResult.type === "string") {
    return <Textarea className={`min-h-0 h-full resize-none font-mono text-xs ${inputClass}`} value={valueDraft} onChange={(event) => onValueDraft(event.target.value)} />;
  }
  return <TerminalBlock surface="log" className="min-h-0 text-xs">{formatRedisValue(keyResult.value)}</TerminalBlock>;
}

function RedisEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-w-0 items-center justify-between gap-3 border-t px-3 py-2 text-xs ${borderClass}`}>
      <span className={`truncate font-mono ${mutedClass}`}>{target.ref}</span>
      <span className={`truncate ${mutedClass}`}>{target.config?.host}:{target.config?.port} db {target.config?.database || 0}</span>
    </div>
  );
}

function formatRedisValue(value) {
  if (typeof value === "string") return value;
  return JSON.stringify(value ?? null, null, 2);
}

function valueToEditableText(output) {
  if (!output) return "";
  if (output.type === "string") return formatEditableString(output.value);
  return formatRedisValue(output.value);
}

function formatEditableString(value) {
  const text = String(value ?? "");
  const trimmed = text.trim();
  if (!trimmed) return text;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return text;
  }
}

function redisScanPattern(value) {
  const trimmed = String(value || "").trim();
  if (!trimmed) return defaultPattern;
  if (/[*?[\]]/.test(trimmed)) return trimmed;
  return `*${trimmed}*`;
}

function keyMetaText(result) {
  const ttl = Number(result.ttl_ms);
  const ttlText = ttl > 0 ? `${Math.ceil(ttl / 1000)}s TTL` : ttl === -1 ? "persistent" : ttl === -2 ? "missing" : "ttl unknown";
  return `${result.type || "unknown"} · ${ttlText}`;
}

function uniqueStrings(values) {
  return Array.from(new Set((values || []).filter(Boolean)));
}
