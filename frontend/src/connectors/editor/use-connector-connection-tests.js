import { useEffect, useRef, useState } from "react";

export function useConnectorConnectionTests({ modelForKind, onOperation, cooldownMs = 1500 }) {
  const [tests, setTests] = useState({});
  const cooldownTimers = useRef(new Map());

  useEffect(() => {
    const timers = cooldownTimers.current;
    return () => {
      for (const timer of timers.values()) window.clearTimeout(timer);
      timers.clear();
    };
  }, []);

  function setResult(testKey, value) {
    const completedAt = Date.now();
    setTests((current) => ({ ...current, [testKey]: { ...value, cooldown: true, completedAt } }));
    window.clearTimeout(cooldownTimers.current.get(testKey));
    const timer = window.setTimeout(() => {
      cooldownTimers.current.delete(testKey);
      setTests((current) => {
        if (current[testKey]?.completedAt !== completedAt) return current;
        return { ...current, [testKey]: { ...current[testKey], cooldown: false } };
      });
    }, cooldownMs);
    cooldownTimers.current.set(testKey, timer);
  }

  async function run(target, profile) {
    const testKey = connectorTestKey(target, profile);
    const model = modelForKind(target.connector_kind);
    if (!model?.test) {
      setTests((current) => ({
        ...current,
        [testKey]: { state: "error", error: `Connector model not found for ${target.connector_kind}.`, data: null },
      }));
      return false;
    }
    if (!profile) {
      setTests((current) => ({
        ...current,
        [testKey]: { state: "error", error: "Select a credential profile before testing.", data: null },
      }));
      return false;
    }
    setTests((current) => ({ ...current, [testKey]: { state: "testing", error: null, data: null } }));
    try {
      const result = await model.test({ target, profile });
      setResult(testKey, { state: result.ok ? "ok" : "error", error: result.error, data: result.data });
      return Boolean(result.ok);
    } catch (error) {
      const operation = model.operationFromError?.(error, { operation: "test", target, profile, testKey });
      if (operation?.open && onOperation?.(operation)) {
        setTests((current) => ({ ...current, [testKey]: { state: "idle", error: null, data: null } }));
        return false;
      }
      setResult(testKey, { state: "error", error: error.message, data: null });
      return false;
    }
  }

  function applyOperationResult(testKey, test) {
    setResult(testKey, { state: test.ok ? "ok" : "error", error: test.error, data: test.data });
  }

  return { tests, run, applyOperationResult };
}

export function connectorTestKey(target, profile) {
  const profileID = profile?.id || "target";
  return `${target.connector_kind}:${target.id}:${profileID}`;
}
