import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { defaultRedisLimit, defaultRedisPattern, redisScanPattern, uniqueRedisKeys, valueToEditableText } from "./browser-helpers";
import { serverProductLabel } from "./model";
import { useRedisMutations } from "./use-redis-mutations";

export function useRedisBrowser({ target, approvals, session, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const resetKey = `${target.ref}:${activeSession.startedAt || "inactive"}`;
  const product = serverProductLabel(target);
  const [pattern, setPattern] = useState(defaultRedisPattern);
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
  const requestGuard = useRequestGuard(resetKey);
  const latestAction = useMemo(
    () => (approvals?.data || []).find((item) => item.target_ref === target.ref) || null,
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
    setState({ state: "idle", error: "", message: "" });
  }, [resetKey]);

  useEffect(() => {
    if (activeSession.active) void scanKeysForEffect({ reset: true });
  }, [activeSession.active, resetKey]);

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
    let item;
    try {
      item = await runRedisAction({
        actionName: "scan_keys",
        input: { pattern: redisScanPattern(pattern), cursor: reset ? "0" : cursor || "0", limit: defaultRedisLimit },
        reason: `manual ${product} browser key scan`,
        busy: "scanning",
      });
    } catch {
      return;
    }
    if (!item) return;
    const output = item.output || {};
    const nextKeys = Array.isArray(output.keys) ? output.keys : [];
    setCursor(String(output.next_cursor || "0"));
    setKeys((current) => uniqueRedisKeys(reset ? nextKeys : [...current, ...nextKeys]));
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
    let item;
    try {
      item = await runRedisAction({
        actionName: "get_key",
        input: { key, limit: 250, max_bytes: 262144 },
        reason: `manual ${product} browser key read`,
        busy: "reading",
      });
    } catch {
      return;
    }
    if (!item) return;
    const output = item.output || {};
    setKeyResult(output);
    setValueDraft(valueToEditableText(output));
    setTTLDraft(output.ttl_ms > 0 ? String(Math.ceil(output.ttl_ms / 1000)) : "");
  }

  function toggleSelection(key) {
    setSelectedKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]));
  }

  const mutations = useRedisMutations({
    resetKey,
    product,
    activeKey,
    keyResult,
    valueDraft,
    newKey,
    newValue,
    ttlDraft,
    selectedKeys,
    setState,
    setNewKey,
    setNewValue,
    setKeys,
    setSelectedKeys,
    setActiveKey,
    setKeyResult,
    setValueDraft,
    runAction: runRedisAction,
    loadKey,
  });

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
    latestAction,
    canSaveString: mutations.creatingKey ? Boolean(newKey.trim()) : keyResult?.type === "string",
    canUpdateTTL: Boolean(activeKey && keyResult && keyResult.type !== "none") && state.state === "idle",
    scanKeys,
    startNewKey,
    loadKey,
    toggleSelection,
    ...mutations,
  };
}

export { formatRedisValue, keyMetaText } from "./browser-helpers";
