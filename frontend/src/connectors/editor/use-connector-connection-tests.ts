import { useEffect, useRef, useState } from "react";
import { errorMessage } from "../../lib/errors";
import { useRequestGuard } from "../../lib/request-guard";

type TargetIdentity = { id: number; connector_kind: string };
type ProfileIdentity = { id: number };
export type ConnectorRecoveryOperation = { open?: boolean; connector_kind?: string; [field: string]: unknown };
export type ConnectorConnectionResult = { ok: boolean; error?: string | null; data?: unknown };
export type ConnectorTestState = {
  state: "idle" | "testing" | "ok" | "error";
  error: string | null;
  data: unknown;
  cooldown?: boolean;
  completedAt?: number;
};
type TestModel<Target, Profile> = {
  test?: (_context: { target: Target; profile: Profile }) => Promise<ConnectorConnectionResult>;
  operationFromError?: (
    _error: unknown,
    _context: { operation: "test"; target: Target; profile: Profile; testKey: string },
  ) => ConnectorRecoveryOperation | null;
};
type Props<Target, Profile> = {
  modelForKind: (_kind: string) => TestModel<Target, Profile> | null | undefined;
  onOperation?: (_operation: ConnectorRecoveryOperation) => boolean | void;
  cooldownMs?: number;
};

export function useConnectorConnectionTests<Target extends TargetIdentity, Profile extends ProfileIdentity>({
  modelForKind,
  onOperation,
  cooldownMs = 1500,
}: Props<Target, Profile>) {
  const [tests, setTests] = useState<Record<string, ConnectorTestState>>({});
  const cooldownTimers = useRef(new Map<string, number>());
  const pendingTests = useRef(new Set<string>());
  const requests = useRequestGuard("connector-connection-tests");

  useEffect(() => {
    const timers = cooldownTimers.current;
    return () => {
      for (const timer of timers.values()) window.clearTimeout(timer);
      timers.clear();
    };
  }, []);

  function setResult(testKey: string, value: ConnectorTestState) {
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

  async function run(target: Target, profile: Profile | null | undefined) {
    const testKey = connectorTestKey(target, profile);
    if (pendingTests.current.has(testKey)) return false;
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
    const request = requests.begin(testKey);
    pendingTests.current.add(testKey);
    setTests((current) => ({ ...current, [testKey]: { state: "testing", error: null, data: null } }));
    try {
      const result = await model.test({ target, profile });
      if (!request.isCurrent()) return false;
      setResult(testKey, { state: result.ok ? "ok" : "error", error: result.error ?? null, data: result.data ?? null });
      return Boolean(result.ok);
    } catch (error) {
      if (!request.isCurrent()) return false;
      const operation = model.operationFromError?.(error, { operation: "test", target, profile, testKey });
      if (operation?.open && onOperation?.(operation)) {
        setTests((current) => ({ ...current, [testKey]: { state: "idle", error: null, data: null } }));
        return false;
      }
      setResult(testKey, { state: "error", error: errorMessage(error), data: null });
      return false;
    } finally {
      pendingTests.current.delete(testKey);
      request.complete();
    }
  }

  function applyOperationResult(testKey: string, test: ConnectorConnectionResult) {
    setResult(testKey, { state: test.ok ? "ok" : "error", error: test.error ?? null, data: test.data ?? null });
  }

  return { tests, run, applyOperationResult };
}

export function connectorTestKey(target: TargetIdentity, profile: ProfileIdentity | null | undefined) {
  const profileID = profile?.id || "target";
  return `${target.connector_kind}:${target.id}:${profileID}`;
}
