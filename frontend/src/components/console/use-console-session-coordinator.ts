import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { failedResource, pollReadOptions } from "../../lib/async-resource";
import { errorMessage } from "../../lib/errors";
import { useRequestGuard } from "../../lib/request-guard";
import { consoleSessions, type ConsoleSession } from "../../lib/gateway-contracts/security-contracts";
import { mergeConsoleSessionData } from "../app-shell-runtime";
import { isLiveConsoleSession, latestSessionForRuntime } from "./helpers";
import { useConsoleConnections } from "./use-console-connections";
import { vaultSessionOptionsResponse } from "../../lib/gateway-contracts/vault-session-options-contract.ts";
import type { VaultSessionOptions } from "../../lib/gateway-contracts/vault-session-options-contract.ts";

type Runtime = { id: number; name: string };
type Session = ConsoleSession & { runtime_id?: number; status?: string; name?: string };
type SessionOptions = {
  name?: string;
  closeExisting?: boolean;
  params?: Record<string, unknown>;
  vaultItems?: readonly Record<string, unknown>[];
  deferActivation?: boolean;
};
type VaultDialog = {
  open: boolean;
  status: string;
  runtime: Runtime | null;
  options: VaultSessionOptions | null;
  sessionOptions: SessionOptions | null;
  error: string | null;
};
type Resource<T> = { state: string; data: T[]; error: string | null };
type Request = ReturnType<ReturnType<typeof useRequestGuard>["begin"]>;

const initialSessions: Resource<Session> = { state: "loading", data: [], error: null };
const initialVaultDialog: VaultDialog = {
  open: false,
  status: "idle",
  runtime: null,
  options: null,
  sessionOptions: null,
  error: null,
};

export function useConsoleSessionCoordinator({ pollIsCurrent }: { pollIsCurrent: (_generation: number | undefined) => boolean }) {
  const [sessions, setSessions] = useState<Resource<Session>>(initialSessions);
  const [vaultDialog, setVaultDialog] = useState<VaultDialog>(initialVaultDialog);
  const vaultResolverRef = useRef<{ resolve: (_session: Session | null) => void } | null>(null);
  const requests = useRequestGuard("console-sessions");
  const { attachSession, closeSession, disconnectAll, disconnectSessions, resizeSession, sendInput } = useConsoleConnections({
    setConsoleSessions: setSessions,
  });

  const loadSessions = useCallback(
    async (generation?: number) => {
      const request = requests.begin("load");
      try {
        const data = await apiGet("/api/console/sessions", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        const verified = consoleSessions(data) as Session[];
        setSessions((current) => ({ state: "ready", data: mergeConsoleSessionData(verified, current.data), error: null }));
        verified.filter((session) => isLiveConsoleSession(session)).forEach((session) => attachSession(session.id));
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setSessions((current) => failedResource(current, error));
      } finally {
        request.complete();
      }
    },
    [attachSession, pollIsCurrent, requests],
  );

  const upsertSession = useCallback((session: Session) => {
    setSessions((current) => {
      const index = current.data.findIndex((item) => Number(item.id) === Number(session.id));
      const data = [...current.data];
      if (index === -1) data.unshift(session);
      else data[index] = { ...data[index], ...session };
      return { state: "ready", data, error: null };
    });
  }, []);

  const activateSession = useCallback(
    (session: Session, request?: Request) => {
      if (request && !request.isCurrent()) return;
      upsertSession(session);
      window.setTimeout(() => {
        if (!request || request.isCurrent()) attachSession(session.id);
      }, 0);
    },
    [attachSession, upsertSession],
  );

  const createSession = useCallback(
    async (runtime: Runtime, options: SessionOptions = {}, request?: Request): Promise<Session | null> => {
      const response = await apiPost(
        "/api/console/sessions",
        {
          runtime_id: runtime.id,
          name: options.name || `${runtime.name} shell`,
          close_existing: options.closeExisting !== false,
          params: options.params || undefined,
          vault_items: options.vaultItems || undefined,
        },
        request ? { signal: request.signal } : undefined,
      );
      if (request && !request.isCurrent()) return null;
      const session = consoleSessions([response])[0] as Session;
      if (!Number.isSafeInteger(session.runtime_id) || session.runtime_id !== runtime.id) {
        throw new Error("Console session runtime does not match the requested runtime.");
      }
      if (!options.deferActivation) activateSession(session, request);
      return session;
    },
    [activateSession],
  );

  const newSession = useCallback(
    async (runtime: Runtime, options: SessionOptions = {}): Promise<Session | null> => {
      const request = requests.begin("new-session");
      requests.invalidate("vault-start");
      const previous = vaultResolverRef.current;
      previous?.resolve(null);
      vaultResolverRef.current = null;
      setVaultDialog(initialVaultDialog);
      try {
        if (options.vaultItems !== undefined) return await createSession(runtime, options, request);
        let vaultOptions: VaultSessionOptions;
        try {
          const response = await apiGet(`/api/vault-session-options?runtime_id=${encodeURIComponent(runtime.id)}`, {
            signal: request.signal,
          });
          if (!request.isCurrent()) return null;
          vaultOptions = vaultSessionOptionsResponse(response);
        } catch {
          if (!request.isCurrent()) return null;
          // Vault selection is optional; a failed probe must not block a normal local console.
          return await createSession(runtime, options, request);
        }
        if (!request.isCurrent()) return null;
        if (vaultOptions.supported && ((vaultOptions.items || []).length > 0 || (vaultOptions.defaults || []).length > 0)) {
          return await new Promise<Session | null>((resolve) => {
            vaultResolverRef.current = { resolve };
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
        return await createSession(runtime, options, request);
      } finally {
        request.complete();
      }
    },
    [createSession, requests],
  );

  const ensureSession = useCallback(
    async (runtime: Runtime) => {
      const current = latestSessionForRuntime(sessions.data, runtime.id);
      if (!current) return newSession(runtime);
      if (isLiveConsoleSession(current)) attachSession(current.id);
      return current;
    },
    [attachSession, newSession, sessions.data],
  );

  const startVaultSession = useCallback(
    async (vaultItems: readonly Record<string, unknown>[]) => {
      const current = vaultDialog;
      if (!current.runtime) return;
      const resolver = vaultResolverRef.current;
      const request = requests.begin("vault-start");
      setVaultDialog((value) => ({ ...value, status: "starting", error: null }));
      try {
        const session = await createSession(
          current.runtime,
          {
            ...current.sessionOptions,
            vaultItems,
            deferActivation: true,
          },
          request,
        );
        if (!request.isCurrent() || !session) return;
        setVaultDialog(initialVaultDialog);
        activateSession(session, request);
        if (vaultResolverRef.current === resolver) {
          resolver?.resolve(session);
          vaultResolverRef.current = null;
        }
      } catch (error) {
        if (!request.isCurrent()) return;
        setVaultDialog((value) => ({ ...value, status: "error", error: errorMessage(error, "Console session could not be started.") }));
      } finally {
        request.complete();
      }
    },
    [activateSession, createSession, requests, vaultDialog],
  );

  const closeVaultDialog = useCallback(() => {
    requests.invalidate("new-session");
    requests.invalidate("vault-start");
    const pending = vaultResolverRef.current;
    pending?.resolve(null);
    vaultResolverRef.current = null;
    setVaultDialog(initialVaultDialog);
  }, [requests]);

  const cancelCommand = useCallback((sessionID: number) => sendInput(sessionID, "\u0003"), [sendInput]);

  const restartRuntime = useCallback(
    async (runtimeID: number) => {
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
