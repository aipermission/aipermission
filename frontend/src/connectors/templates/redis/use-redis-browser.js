import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useRequestGuard } from "../_shared/request-guard";
import { serverProductLabel, validateStringWrite } from "./model";

const defaultPattern = "*";
const defaultLimit = 100;

export const emptyConfirmDialog = Object.freeze({
  open: false,
  type: "",
  title: "",
  description: "",
  details: [],
  tone: "warn",
  pending: false,
  error: "",
  onConfirm: null,
});

export function useRedisBrowser({ target, approvals, session, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const product = serverProductLabel(target);
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
  const [confirmDialog, setConfirmDialog] = useState(emptyConfirmDialog);
  const requestGuard = useRequestGuard(`${target.ref}:${activeSession.startedAt || "inactive"}`);
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const scanKeysForEffect = useEffectEvent((options) => scanKeys(options));

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
    void scanKeysForEffect({ reset: true });
  }, [activeSession.active, activeSession.startedAt, target.ref]);

  async function runRedisAction({ actionName, input, reason, busy = "running", channel = actionName }) {
    return runGuardedConnectorAction({
      requestGuard,
      channel,
      targetRef: target.ref,
      actionName,
      input,
      reason,
      busy,
      product,
      setState,
      onRefreshActivity,
    });
  }

  async function scanKeys({ reset = false } = {}) {
    if (!activeSession.active) return;
    const startCursor = reset ? "0" : cursor || "0";
    const item = await runRedisAction({
      actionName: "scan_keys",
      input: { pattern: redisScanPattern(pattern), cursor: startCursor, limit: defaultLimit },
      reason: `manual ${product} browser key scan`,
      busy: "scanning",
    });
    if (!item) return;
    const output = item.output || {};
    const nextKeys = Array.isArray(output.keys) ? output.keys : [];
    setCursor(String(output.next_cursor || "0"));
    setKeys((current) => uniqueStrings(reset ? nextKeys : [...current, ...nextKeys]));
  }

  function startNewKey() {
    requestGuard.invalidate("get_key");
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
      reason: `manual ${product} browser key read`,
      busy: "reading",
    });
    if (!item) return;
    const output = item.output || {};
    setKeyResult(output);
    setValueDraft(valueToEditableText(output));
    setTTLDraft(output.ttl_ms && output.ttl_ms > 0 ? String(Math.ceil(output.ttl_ms / 1000)) : "");
  }

  function saveStringValue(event) {
    event?.preventDefault?.();
    const key = activeKey || newKey.trim();
    const value = activeKey ? valueDraft : newValue;
    const validationError = validateStringWrite({ key, value });
    if (validationError) {
      setState({ state: "idle", error: validationError, message: "" });
      return;
    }
    const ttlSeconds = Number(ttlDraft) > 0 ? Number(ttlDraft) : 0;
    openConfirmDialog({
      type: "save-string",
      title: activeKey ? `Save ${product} string` : `Create ${product} string key`,
      description: activeKey ? `This will overwrite the selected key as a ${product} string.` : `This will create a ${product} string key.`,
      tone: "warn",
      details: [
        { label: "Key", value: key },
        { label: "TTL", value: ttlSeconds > 0 ? `${ttlSeconds}s` : "persistent" },
      ],
      onConfirm: async () => {
        const written = await runRedisAction({
          actionName: "set_string",
          input: { key, value, ttl_seconds: ttlSeconds },
          reason: `manual ${product} browser string write`,
          busy: "writing",
        });
        if (!written) return false;
        setNewKey("");
        setNewValue("");
        setKeys((current) => uniqueStrings([...current, key]).sort());
        await loadKey(key);
        return true;
      },
    });
  }

  function updateTTL() {
    if (!activeKey) return;
    const ttlSeconds = ttlDraft.trim() === "" ? -1 : Number(ttlDraft);
    const normalizedTTL = Number.isFinite(ttlSeconds) ? ttlSeconds : -1;
    openConfirmDialog({
      type: "ttl",
      title: normalizedTTL < 0 ? `Persist ${product} key` : `Update ${product} TTL`,
      description: normalizedTTL < 0 ? "This removes the expiration from the selected key." : "This changes when the selected key expires.",
      tone: "warn",
      details: [
        { label: "Key", value: activeKey },
        { label: "TTL", value: normalizedTTL < 0 ? "persistent" : `${normalizedTTL}s` },
      ],
      onConfirm: async () => {
        const updated = await runRedisAction({
          actionName: "expire_key",
          input: { key: activeKey, ttl_seconds: normalizedTTL },
          reason: `manual ${product} browser TTL update`,
          busy: "writing",
        });
        if (!updated) return false;
        await loadKey(activeKey);
        return true;
      },
    });
  }

  function deleteSelected() {
    const keysToDelete = selectedKeys.length > 0 ? selectedKeys : activeKey ? [activeKey] : [];
    if (keysToDelete.length === 0) return;
    openConfirmDialog({
      type: "delete",
      title: `Delete ${keysToDelete.length} ${product} key${keysToDelete.length === 1 ? "" : "s"}`,
      description: `This permanently deletes the selected ${product} key data.`,
      tone: "bad",
      details: keysToDelete
        .slice(0, 8)
        .map((key) => ({ label: "Key", value: key }))
        .concat(keysToDelete.length > 8 ? [{ label: "More", value: `${keysToDelete.length - 8} additional key(s)` }] : []),
      onConfirm: async () => {
        const deleted = await runRedisAction({
          actionName: "delete_keys",
          input: { keys: keysToDelete },
          reason: `manual ${product} browser key delete`,
          busy: "deleting",
        });
        if (!deleted) return false;
        setKeys((current) => current.filter((key) => !keysToDelete.includes(key)));
        setSelectedKeys([]);
        if (keysToDelete.includes(activeKey)) {
          setActiveKey("");
          setKeyResult(null);
          setValueDraft("");
        }
        return true;
      },
    });
  }

  function openConfirmDialog({ type, title, description, details, tone, onConfirm }) {
    setConfirmDialog({ open: true, type, title, description, details, tone, pending: false, error: "", onConfirm });
  }

  async function confirmPendingAction() {
    if (!confirmDialog.onConfirm) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    try {
      const completed = await confirmDialog.onConfirm();
      if (completed === false) {
        setConfirmDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setConfirmDialog(emptyConfirmDialog);
    } catch (error) {
      setConfirmDialog((current) => ({ ...current, pending: false, error: error.message || `${product} action failed.` }));
    }
  }

  function toggleSelection(key) {
    setSelectedKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]));
  }

  const creatingKey = !activeKey;
  const editableString = creatingKey || keyResult?.type === "string";
  return {
    activeSession,
    product,
    pattern,
    setPattern,
    cursor,
    keys,
    selectedKeys,
    setSelectedKeys,
    selectedCount: selectedKeys.length,
    activeKey,
    keyResult,
    valueDraft,
    setValueDraft,
    newKey,
    setNewKey,
    newValue,
    setNewValue,
    ttlDraft,
    setTTLDraft,
    state,
    resultMode,
    setResultMode,
    confirmDialog,
    closeConfirmDialog: () => setConfirmDialog(emptyConfirmDialog),
    latestAction: activeItems[0] || null,
    creatingKey,
    editableString,
    canSaveString: creatingKey ? Boolean(newKey.trim()) : keyResult?.type === "string",
    canUpdateTTL: Boolean(activeKey && keyResult && keyResult.type !== "none") && state.state === "idle",
    scanKeys,
    startNewKey,
    loadKey,
    saveStringValue,
    updateTTL,
    deleteSelected,
    confirmPendingAction,
    toggleSelection,
  };
}

export function formatRedisValue(value) {
  if (typeof value === "string") return value;
  return JSON.stringify(value ?? null, null, 2);
}

export function keyMetaText(result) {
  const ttl = Number(result.ttl_ms);
  const ttlText = ttl > 0 ? `${Math.ceil(ttl / 1000)}s TTL` : ttl === -1 ? "persistent" : ttl === -2 ? "missing" : "ttl unknown";
  return `${result.type || "unknown"} · ${ttlText}`;
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

function uniqueStrings(values) {
  return Array.from(new Set((values || []).filter(Boolean)));
}
