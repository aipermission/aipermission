import { useCallback, useEffect, useEffectEvent, useMemo, useState } from "react";
import { getConnectorModel } from "../../connectors/templates/registry";
import { useRequestGuard } from "../../lib/request-guard";
import { isLiveConsoleSession } from "./helpers";

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
}) {
  const [structuredByTarget, setStructuredByTarget] = useState({});
  const [liveSessionNameByTarget, setLiveSessionNameByTarget] = useState({});
  const [restartAction, setRestartAction] = useState({ state: "idle", error: null });
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
  const attachSelectedSession = useEffectEvent((sessionID) => attachConsoleSession(sessionID));

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
    async (runtimeTarget, options = {}) => {
      if (!runtimeTarget) return;
      const request = requests.begin("new-session");
      setNewSessionError("");
      if (options.name && selectedTarget?.ref) {
        setLiveSessionNameByTarget((current) => ({ ...current, [selectedTarget.ref]: options.name }));
      }
      try {
        await newConsoleSession(runtimeTarget, options);
      } catch (error) {
        if (!request.isCurrent()) return;
        const model = resolveConnectorModel(runtimeTarget.connector_kind);
        const operation = model?.operationFromError?.(error, { operation: "new-session", target: runtimeTarget });
        if (onOpenConnectorOperation(operation)) return;
        setNewSessionError(error.message || "Console session could not be started.");
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
      setRestartAction({ state: "error", error: error.message });
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
    (name) => {
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

function newStructuredConsoleSession() {
  return { active: true, startedAt: new Date().toISOString() };
}
