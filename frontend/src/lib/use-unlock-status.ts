import { useEffect, useRef, useState } from "react";
import { apiGet } from "./api";
import { databaseStatusResponse, type DatabaseStatus } from "./gateway-contracts/database-status-contract.ts";
import { errorMessage } from "./errors.ts";

export function useUnlockStatus() {
  const [unlock, setUnlock] = useState<
    { state: "loading"; data: null; error: null } |
    { state: "ready"; data: DatabaseStatus; error: null } |
    { state: "error"; data: null; error: string }
  >({ state: "loading", data: null, error: null });
  const unlockLoadGeneration = useRef(0);

  async function loadUnlockStatus(signal?: AbortSignal) {
    const generation = ++unlockLoadGeneration.current;
    try {
      const response: unknown = await apiGet("/api/unlock/status", { signal });
      if (signal?.aborted || generation !== unlockLoadGeneration.current) return;
      const data = databaseStatusResponse(response);
      if (typeof data.state !== "string" || data.state.length === 0) throw new Error("Invalid unlock status response.");
      setUnlock({ state: "ready", data, error: null });
    } catch (error) {
      if (signal?.aborted || generation !== unlockLoadGeneration.current) return;
      if (signal) throw error;
      setUnlock({ state: "error", data: null, error: errorMessage(error, "Could not load database status.") });
    }
  }

  useEffect(() => {
    void loadUnlockStatus();
  }, []);

  useEffect(() => {
    function handleSessionRequired() {
      void loadUnlockStatus();
    }
    window.addEventListener("aipermission:ui-session-required", handleSessionRequired);
    return () => window.removeEventListener("aipermission:ui-session-required", handleSessionRequired);
  }, []);

  return { unlock, loadUnlockStatus };
}
