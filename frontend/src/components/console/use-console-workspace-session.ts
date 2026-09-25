import { useCallback, useEffect, useEffectEvent, useMemo, useState } from "react";
import { getConnectorModel } from "../../connectors/templates/registry";
import { useRequestGuard } from "../../lib/request-guard";
import { errorMessage } from "../../lib/errors";
import { isLiveConsoleSession } from "./helpers";

type Target = { ref: string; connector_kind?: string };
type RuntimeTarget = { id: number; connector_kind?: string };
type Session = { id: number; runtime_id?: number; name?: string; status?: string; error?: string | null };
type StructuredSession = { active: boolean; startedAt: string };
type StartOptions = Record<string, unknown> & { name?: string };
type ConnectorModel = {
  operationFromError?: (_error: unknown, _context: { operation: string; target: RuntimeTarget }) => unknown;
};
type Props = {
  attachConsoleSession: (_sessionID: number) => void | Promise<unknown>;
  newConsoleSession: (_runtimeTarget: RuntimeTarget, _options: StartOptions) => Promise<unknown>;
  onOpenConnectorOperation: (_operation: unknown) => boolean;
  restartConsoleRuntime: (_runtimeID: number) => Promise<unknown>;
  runtimeSelectedSession: Session;
  selectedRunningRequestID?: number | string | null;
  selectedRuntimeTarget: RuntimeTarget | null;
  selectedTarget: Target | null;
  selectedTargetUsesLiveConsole: boolean;
  sessions: readonly Session[];
  resolveConnectorModel?: (_kind: string | undefined) => ConnectorModel | null;
};

export function useConsoleWorkspaceSession({
  attachConsoleSession,
  newConsoleSession,
  onOpenConnectorOperation,
  restartConsoleRuntime,
  runtimeSelectedSession,
  selectedRunningRequestID,
  selectedRuntimeTarget,
  selectedTarget,
  selectedTargetUsesLiveConsole,
  sessions,
  resolveConnectorModel = getConnectorModel,
}: Props) {
  const [structuredByTarget, setStructuredByTarget] = useState<Record<string, StructuredSession>>({});
  const [liveSessionNameByTarget, setLiveSessionNameByTarget] = useState<Record<string, string>>({});
  const [restartAction, setRestartAction] = useState<{ state: "idle" | "running" | "error"; error: string | null }>({
    state: "idle",
    error: null,
  });
  const [newSessionError, setNewSessionError] = useState("");
  const requests = useRequestGuard(`console-workspace:${selectedTarget?.ref || "none"}:${selectedRuntimeTarget?.id || "none"}`);
  const selectedStructuredSession =
    selectedTarget && !selectedTargetUsesLiveConsole ? structuredByTarget[selectedTarget.ref] || null : null;
  const selectedLiveSessionName = selectedTarget?.ref ? liveSessionNameByTarget[selectedTarget.ref] || "" : "";
  const selectedNamedLiveSession = useMemo(
    () =>
      selectedRuntimeTarget && selectedLiveSessionName
        ? sessions.find(
            (session) => Number(session.runtime_id) === Number(selectedRuntimeTarget.id) && session.name === selectedLiveSessionName,
          ) || null
        : null,
    [selectedLiveSessionName, selectedRuntimeTarget, sessions],
  );
  const selectedSession = selectedNamedLiveSession || runtimeSelectedSession;
  const selectedSessionLive = isLiveConsoleSession(selectedSession);
  const attachSelectedSession = useEffectEvent((sessionID: number) => attachConsoleSession(sessionID));

  useEffect(() => {
    if (!selectedTarget || selectedTargetUsesLiveConsole) return;
    setStructuredByTarget((current) => {
      if (current[selectedTarget.ref]) return current;
      return { ...current, [selectedTarget.ref]: newStructuredConsoleSession() };
    });
  }, [selectedTarget, selectedTargetUsesLiveConsole]);

  useEffect(() => {
    if (selectedRuntimeTarget && selectedSessionLive) attachSelectedSession(selectedSession.id);
  }, [selectedRuntimeTarget, selectedSession.id, selectedSessionLive]);

  useEffect(() => {
    setRestartAction({ state: "idle", error: null });
    setNewSessionError("");
  }, [selectedRunningRequestID, selectedRuntimeTarget?.id]);

  const startNew = useCallback(
    async (runtimeTarget: RuntimeTarget | null, options: StartOptions = {}) => {
      if (!runtimeTarget) return;
      const request = requests.begin("new-session");
      setNewSessionError("");
      const sessionName = options.name;
      const targetRef = selectedTarget?.ref;
      if (sessionName && targetRef) {
        setLiveSessionNameByTarget((current) => ({ ...current, [targetRef]: sessionName }));
      }
      try {
        await newConsoleSession(runtimeTarget, options);
      } catch (error) {
        if (!request.isCurrent()) return;
        const model = resolveConnectorModel(runtimeTarget.connector_kind);
        const operation = model?.operationFromError?.(error, { operation: "new-session", target: runtimeTarget });
        if (onOpenConnectorOperation(operation)) return;
        setNewSessionError(errorMessage(error, "Console session could not be started."));
      } finally {
        request.complete();
      }
    },
    [newConsoleSession, onOpenConnectorOperation, requests, resolveConnectorModel, selectedTarget?.ref],
  );

  const restart = useCallback(async () => {
    if (!selectedRuntimeTarget) return;
    const request = requests.begin("restart");
    setRestartAction({ state: "running", error: null });
    try {
      await restartConsoleRuntime(selectedRuntimeTarget.id);
      if (!request.isCurrent()) return;
      setRestartAction({ state: "idle", error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setRestartAction({ state: "error", error: errorMessage(error, "Console runtime could not be restarted.") });
    } finally {
      request.complete();
    }
  }, [requests, restartConsoleRuntime, selectedRuntimeTarget]);

  const startStructured = useCallback(() => {
    if (!selectedTarget || selectedTargetUsesLiveConsole) return;
    setStructuredByTarget((current) => ({ ...current, [selectedTarget.ref]: newStructuredConsoleSession() }));
  }, [selectedTarget, selectedTargetUsesLiveConsole]);

  const endStructured = useCallback(() => {
    if (!selectedTarget || selectedTargetUsesLiveConsole) return;
    setStructuredByTarget((current) => ({ ...current, [selectedTarget.ref]: { active: false, startedAt: "" } }));
  }, [selectedTarget, selectedTargetUsesLiveConsole]);

  const selectLiveSessionName = useCallback(
    (name: string) => {
      if (!selectedTarget?.ref || !name) return;
      setLiveSessionNameByTarget((current) => ({ ...current, [selectedTarget.ref]: name }));
    },
    [selectedTarget?.ref],
  );

  return {
    endStructured,
    newSessionError,
    restart,
    restartAction,
    selectLiveSessionName,
    selectedSession,
    selectedSessionLive,
    selectedStructuredSession,
    startNew,
    startStructured,
  };
}

function newStructuredConsoleSession(): StructuredSession {
  return { active: true, startedAt: new Date().toISOString() };
}
