import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet } from "./api";
import { databaseStatusResponse, type DatabaseStatus } from "./gateway-contracts/database-status-contract.ts";
import { errorMessage } from "./errors.ts";
import { subscribeUISessionInvalidation } from "./ui-session-events.ts";

export function useUnlockStatus() {
  const [unlock, setUnlock] = useState<
    | { state: "loading"; data: null; error: null }
    | { state: "ready"; data: DatabaseStatus; error: null }
    | { state: "error"; data: null; error: string }
  >({ state: "loading", data: null, error: null });
  const ownership = useRef<{ mounted: boolean; generation: number; pending: AbortController | null }>({
    mounted: false,
    generation: 0,
    pending: null,
  });

  const loadUnlockStatus = useCallback(async (signal?: AbortSignal) => {
    const owner = ownership.current;
    if (!owner.mounted || signal?.aborted) return;
    const generation = ++owner.generation;
    owner.pending?.abort();
    const controller = new AbortController();
    owner.pending = controller;
    const abort = () => controller.abort();
    signal?.addEventListener("abort", abort, { once: true });
    try {
      const response: unknown = await apiGet("/api/unlock/status", { signal: controller.signal, timeoutMs: 4000 });
      if (controller.signal.aborted || !owner.mounted || generation !== owner.generation) return;
      const data = databaseStatusResponse(response);
      if (typeof data.state !== "string" || data.state.length === 0) throw new Error("Invalid unlock status response.");
      setUnlock({ state: "ready", data, error: null });
    } catch (error) {
      if (controller.signal.aborted || !owner.mounted || generation !== owner.generation) return;
      if (signal) throw error;
      setUnlock({ state: "error", data: null, error: errorMessage(error, "Could not load database status.") });
    } finally {
      signal?.removeEventListener("abort", abort);
      if (owner.pending === controller) owner.pending = null;
    }
  }, []);

  useEffect(() => {
    const owner = ownership.current;
    owner.mounted = true;
    void loadUnlockStatus();
    const unsubscribe = subscribeUISessionInvalidation(() => {
      setUnlock((current) => (current.data?.state === "unlocked" ? { state: "loading", data: null, error: null } : current));
      void loadUnlockStatus();
    });
    function refresh() {
      if (document.visibilityState === "visible" && !owner.pending) void loadUnlockStatus();
    }
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    const timer = window.setInterval(refresh, 30000);
    return () => {
      owner.mounted = false;
      owner.generation++;
      owner.pending?.abort();
      window.clearInterval(timer);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
      unsubscribe();
    };
  }, [loadUnlockStatus]);

  return { unlock, loadUnlockStatus };
}
