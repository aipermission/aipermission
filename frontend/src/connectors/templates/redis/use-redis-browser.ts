import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { currentWorkspaceBinding } from "../../../lib/api";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useConnectorMutationOwnership } from "../_shared/use-connector-mutation-ownership";
import { defaultRedisLimit, defaultRedisPattern, redisScanPattern, uniqueRedisKeys, valueToEditableText } from "./browser-helpers";
import { serverProductLabel } from "./model";
import { useRedisMutations } from "./use-redis-mutations";
import { readRedisKey, readRedisScan } from "./browser-output";
import type { RedisActionOptions, RedisBrowserProps, RedisKeyResult } from "./browser-types";

const mutationActions = ["set_string", "expire_key", "delete_keys"] as const;

export function useRedisBrowser({ target, approvals, session, onRefreshActivity }: RedisBrowserProps) {
  const activeSession = session || { active: false, startedAt: "" };
  const resetKey = `${currentWorkspaceBinding()}:${target.ref}:${activeSession.startedAt || "inactive"}`;
  const product = serverProductLabel(target);
  const [pattern, setPatternDraft] = useState(defaultRedisPattern);
  const [appliedPattern, setAppliedPattern] = useState("");
  const [cursor, setCursor] = useState("0");
  const [keys, setKeys] = useState<string[]>([]);
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [selectionEpoch, setSelectionEpoch] = useState(0);
  const [activeKey, setActiveKey] = useState("");
  const [keyResult, setKeyResult] = useState<RedisKeyResult | null>(null);
  const [valueDraft, setValueDraft] = useState("");
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [ttlDraft, setTTLDraft] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const [resultMode, setResultMode] = useState("value");
  const requestGuard = useRequestGuard(resetKey);
  const mutationsOwner = useConnectorMutationOwnership(target.ref, mutationActions, approvals?.state);
  const latestAction = useMemo(
    () => (approvals?.data || []).find((item) => item.target_ref === target.ref) || null,
    [approvals?.data, target.ref],
  );
  const scanKeysForEffect = useEffectEvent((options: { reset?: boolean }) => scanKeys(options));

  useEffect(() => {
    setAppliedPattern("");
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

  async function runRedisAction({ actionName, input, reason, busy = "running", channel = actionName, onPending }: RedisActionOptions) {
    const execute = (pending = onPending) =>
      runGuardedConnectorAction({
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
        onPending: pending,
        exclusiveMutationActions: mutationActions.some((name) => name === actionName) ? mutationActions : undefined,
      });
    return mutationActions.some((name) => name === actionName) ? mutationsOwner.run(execute) : execute();
  }

  async function scanKeys({ reset = false } = {}) {
    if (!activeSession.active) return;
    const query = redisScanPattern(pattern);
    if (!reset && (query !== appliedPattern || cursor === "0")) return;
    if (reset) {
      setAppliedPattern(query);
      setCursor("0");
      setKeys([]);
      clearKeySelection();
    }
    let item;
    try {
      item = await runRedisAction({
        actionName: "scan_keys",
        input: { pattern: query, cursor: reset ? "0" : cursor, limit: defaultRedisLimit },
        reason: `manual ${product} browser key scan`,
        busy: "scanning",
      });
    } catch {
      return;
    }
    if (!item) return;
    const output = readRedisScan(item.output);
    setCursor(output.nextCursor);
    setKeys((current) => uniqueRedisKeys(reset ? output.keys : [...current, ...output.keys]));
  }

  function clearKeySelection() {
    setSelectionEpoch((current) => current + 1);
    requestGuard.invalidate("get_key");
    setSelectedKeys([]);
    setActiveKey("");
    setKeyResult(null);
    setValueDraft("");
    setTTLDraft("");
  }

  function setPattern(value: string) {
    setPatternDraft(value);
    if (redisScanPattern(value) === redisScanPattern(pattern)) return;
    requestGuard.invalidate("scan_keys");
    setAppliedPattern("");
    setCursor("0");
    setKeys([]);
    clearKeySelection();
    if (state.state === "reading" || state.state === "scanning") setState({ state: "idle", error: "", message: "" });
  }

  function startNewKey() {
    if (state.state !== "idle" && state.state !== "error" && state.state !== "reading") return;
    requestGuard.invalidate("get_key");
    setActiveKey("");
    setKeyResult(null);
    setValueDraft("");
    setNewKey("");
    setNewValue("");
    setTTLDraft("");
    setResultMode("value");
    if (state.state === "reading") setState({ state: "idle", error: "", message: "" });
  }

  async function loadKey(key: string) {
    if (!activeSession.active || !key) return;
    setActiveKey(key);
    setKeyResult(null);
    setValueDraft("");
    setTTLDraft("");
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
    const output = readRedisKey(item.output);
    if (!output || output.key !== key) {
      setState({ state: "error", error: `${product} returned a different key than the one requested.`, message: "" });
      return;
    }
    setKeyResult(output);
    setValueDraft(valueToEditableText(output));
    setTTLDraft(output.ttl_ms !== undefined && output.ttl_ms > 0 ? String(Math.ceil(output.ttl_ms / 1000)) : "");
  }

  function toggleSelection(key: string) {
    setSelectedKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]));
  }

  const mutations = useRedisMutations({
    resetKey: `${resetKey}:${selectionEpoch}`,
    mutationLocked: mutationsOwner.locked,
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
  const activeResultIsCurrent = Boolean(activeKey && keyResult?.key === activeKey);
  const activeStringIsEditable = activeResultIsCurrent && keyResult?.type === "string" && keyResult.truncated !== true;

  return {
    activeSession,
    product,
    pattern,
    setPattern,
    cursor,
    canScanMore: cursor !== "0" && appliedPattern === redisScanPattern(pattern),
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
    canSaveString: !mutationsOwner.locked && (mutations.creatingKey ? newKey !== "" : activeStringIsEditable),
    canUpdateTTL: !mutationsOwner.locked && activeResultIsCurrent && keyResult?.type !== "none" && state.state === "idle",
    mutationLocked: mutationsOwner.locked,
    canStartNewKey: state.state === "idle" || state.state === "error" || state.state === "reading",
    activeStringIsEditable,
    scanKeys,
    startNewKey,
    loadKey,
    toggleSelection,
    ...mutations,
  };
}

export { formatRedisValue, keyMetaText } from "./browser-helpers";
export type RedisBrowser = ReturnType<typeof useRedisBrowser>;
