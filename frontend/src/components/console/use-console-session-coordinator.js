import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
import { mergeConsoleSessionData } from "../app-shell-runtime";
import { isLiveConsoleSession, latestSessionForRuntime } from "./helpers";
import { useConsoleConnections } from "./use-console-connections";

const initialSessions = { state: "loading", data: [], error: null };
const initialVaultDialog = {
  open: false,
  status: "idle",
  runtime: null,
  options: null,
  sessionOptions: null,
  error: null,
};

export function useConsoleSessionCoordinator({ pollIsCurrent }) {
  const [sessions, setSessions] = useState(initialSessions);
  const [vaultDialog, setVaultDialog] = useState(initialVaultDialog);
  const vaultResolverRef = useRef(null);
  const requests = useRequestGuard("console-sessions");
  const { attachSession, closeSession, disconnectAll, disconnectSessions, resizeSession, sendInput } = useConsoleConnections({
    setConsoleSessions: setSessions,
  });

  const loadSessions = useCallback(
    async (generation) => {
      const request = requests.begin("load");
      try {
        const data = await apiGet("/api/console/sessions", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setSessions((current) => ({ state: "ready", data: mergeConsoleSessionData(data, current.data), error: null }));
        data.filter((session) => isLiveConsoleSession(session)).forEach((session) => attachSession(session.id));
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setSessions({ state: "error", data: [], error: error.message });
      } finally {
        request.complete();
      }
    },
    [attachSession, pollIsCurrent, requests],
  );

  const upsertSession = useCallback((session) => {
    setSessions((current) => {
      const index = current.data.findIndex((item) => Number(item.id) === Number(session.id));
      const data = [...current.data];
      if (index === -1) data.unshift(session);
      else data[index] = { ...data[index], ...session };
      return { state: "ready", data, error: null };
    });
  }, []);

  const activateSession = useCallback(
    (session) => {
      upsertSession(session);
      window.setTimeout(() => attachSession(session.id), 0);
    },
    [attachSession, upsertSession],
  );

  const createSession = useCallback(
    async (runtime, options = {}) => {
      const session = await apiPost("/api/console/sessions", {
        runtime_id: runtime.id,
        name: options.name || `${runtime.name} shell`,
        close_existing: options.closeExisting !== false,
        params: options.params || undefined,
        vault_items: options.vaultItems || undefined,
      });
      if (!options.deferActivation) activateSession(session);
      return session;
    },
    [activateSession],
  );

  const newSession = useCallback(
    async (runtime, options = {}) => {
      if (options.vaultItems !== undefined) return createSession(runtime, options);
      const channel = `vault-options:${runtime.id}`;
      const request = requests.begin(channel);
      try {
        const vaultOptions = await apiGet(`/api/vault-session-options?runtime_id=${encodeURIComponent(runtime.id)}`, {
          signal: request.signal,
        });
        if (!request.isCurrent()) return null;
        if (vaultOptions.supported && ((vaultOptions.items || []).length > 0 || (vaultOptions.defaults || []).length > 0)) {
          requests.invalidate("vault-start");
          const previous = vaultResolverRef.current;
          if (previous) {
            requests.invalidate(previous.channel);
            previous.resolve(null);
          }
          return new Promise((resolve) => {
            vaultResolverRef.current = { channel, resolve };
            setVaultDialog({
              open: true,
              status: "idle",
              runtime,
              options: vaultOptions,
              sessionOptions: options,
              error: null,
            });
          });
        }
      } catch {
        if (!request.isCurrent()) return null;
        // Vault selection is optional; a failed probe must not block a normal local console.
      } finally {
        request.complete();
      }
      return createSession(runtime, options);
    },
    [createSession, requests],
  );

  const ensureSession = useCallback(
    async (runtime) => {
      const current = latestSessionForRuntime(sessions.data, runtime.id);
      if (!current) return newSession(runtime);
      if (isLiveConsoleSession(current)) attachSession(current.id);
      return current;
    },
    [attachSession, newSession, sessions.data],
  );

  const startVaultSession = useCallback(
    async (vaultItems) => {
      const current = vaultDialog;
      if (!current.runtime) return;
      const resolver = vaultResolverRef.current;
      const request = requests.begin("vault-start");
      setVaultDialog((value) => ({ ...value, status: "starting", error: null }));
      try {
        const session = await createSession(current.runtime, {
          ...current.sessionOptions,
          vaultItems,
          deferActivation: true,
        });
        if (!request.isCurrent()) return;
        setVaultDialog(initialVaultDialog);
        window.setTimeout(() => activateSession(session), 0);
        if (vaultResolverRef.current === resolver) {
          resolver?.resolve(session);
          vaultResolverRef.current = null;
        }
      } catch (error) {
        if (!request.isCurrent()) return;
        setVaultDialog((value) => ({ ...value, status: "error", error: error.message }));
      } finally {
        request.complete();
      }
    },
    [activateSession, createSession, requests, vaultDialog],
  );

  const closeVaultDialog = useCallback(() => {
    requests.invalidate("vault-start");
    const pending = vaultResolverRef.current;
    if (pending) requests.invalidate(pending.channel);
    pending?.resolve(null);
    vaultResolverRef.current = null;
    setVaultDialog(initialVaultDialog);
  }, [requests]);

  const cancelCommand = useCallback((sessionID) => sendInput(sessionID, "\u0003"), [sendInput]);

  const restartRuntime = useCallback(
    async (runtimeID) => {
      const affected = sessions.data.filter((session) => Number(session.runtime_id) === Number(runtimeID));
      disconnectSessions(affected.map((session) => session.id));
      const result = await apiPost(`/api/console/runtime-surfaces/${runtimeID}/restart`, {});
      await loadSessions();
      return result;
    },
    [disconnectSessions, loadSessions, sessions.data],
  );

  useEffect(
    () => () => {
      vaultResolverRef.current?.resolve(null);
      vaultResolverRef.current = null;
    },
    [],
  );

  return {
    attachSession,
    cancelCommand,
    closeSession,
    closeVaultDialog,
    disconnectAll,
    disconnectSessions,
    ensureSession,
    loadSessions,
    newSession,
    restartRuntime,
    resizeSession,
    sendInput,
    sessions,
    startVaultSession,
    vaultDialog,
  };
}
