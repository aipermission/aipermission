import { useCallback, useState } from "react";
import type { FormEvent } from "react";
import { apiGet, apiPost } from "../lib/api";
import { pollReadOptions } from "../lib/async-resource";
import { databaseStatusResponse } from "../lib/gateway-contracts/database-status-contract.ts";
import type { DatabaseStatus } from "../lib/gateway-contracts/database-status-contract.ts";
import { errorMessage } from "../lib/errors.ts";

type SwitchDialogState = {
  open: boolean;
  database_id: string;
  password: string;
  state: "idle" | "switching" | "error";
  error: string | null;
};
type LockDialogState = { open: boolean; state: "idle" | "locking" | "error"; error: string | null };
type Props = { disconnectAllConsoleSessions: () => void; pollIsCurrent: (_generation?: number) => boolean };
const initialSwitchDialog: SwitchDialogState = { open: false, database_id: "", password: "", state: "idle", error: null };
const initialLockDialog: LockDialogState = { open: false, state: "idle", error: null };

export function useDatabaseLifecycle({ disconnectAllConsoleSessions, pollIsCurrent }: Props) {
  const [status, setStatus] = useState<{ state: "loading" | "ready" | "error"; data: DatabaseStatus | null; error: string | null }>({
    state: "loading",
    data: null,
    error: null,
  });
  const [switchDialog, setSwitchDialog] = useState(initialSwitchDialog);
  const [lockDialog, setLockDialog] = useState(initialLockDialog);

  const loadStatus = useCallback(
    async (generation?: number) => {
      try {
        const response: unknown = await apiGet("/api/unlock/status", pollReadOptions(undefined, generation));
        if (!pollIsCurrent(generation)) return;
        const data = databaseStatusResponse(response);
        setStatus({ state: "ready", data, error: null });
      } catch (error) {
        if (!pollIsCurrent(generation)) return;
        setStatus((current) => ({ state: "error", data: current.data, error: errorMessage(error, "Could not read database status.") }));
      }
    },
    [pollIsCurrent],
  );

  const lock = useCallback(
    async (scope: "current" | "all") => {
      setLockDialog((current) => ({ ...current, state: "locking", error: null }));
      try {
        await apiPost("/api/lock", { scope });
        disconnectAllConsoleSessions();
        window.location.reload();
      } catch (error) {
        setLockDialog((current) => ({
          ...current,
          open: true,
          state: "error",
          error: errorMessage(error, "Could not lock the database."),
        }));
      }
    },
    [disconnectAllConsoleSessions],
  );

  const requestLock = useCallback(() => {
    const unlockedCount = (status.data?.databases || []).filter((item) => item.unlocked).length;
    if (unlockedCount > 1) {
      setLockDialog({ open: true, state: "idle", error: null });
      return;
    }
    void lock("current");
  }, [lock, status.data?.databases]);

  const openSwitch = useCallback(() => {
    setSwitchDialog({
      open: true,
      database_id: status.data?.database_id || status.data?.databases?.[0]?.id || "",
      password: "",
      state: "idle",
      error: null,
    });
  }, [status.data]);

  const switchDatabase = useCallback(
    async (event?: FormEvent<HTMLFormElement>) => {
      event?.preventDefault?.();
      if (switchDialog.database_id === status.data?.database_id) {
        setSwitchDialog((current) => ({ ...current, open: false }));
        return;
      }
      setSwitchDialog((current) => ({ ...current, state: "switching", error: null }));
      try {
        await apiPost("/api/databases/switch", {
          database_id: switchDialog.database_id,
          password: switchDialog.password,
        });
        disconnectAllConsoleSessions();
        window.location.reload();
      } catch (error) {
        setSwitchDialog((current) => ({ ...current, state: "error", error: errorMessage(error, "Could not switch databases.") }));
      }
    },
    [disconnectAllConsoleSessions, status.data?.database_id, switchDialog.database_id, switchDialog.password],
  );

  const closeLock = useCallback(() => setLockDialog(initialLockDialog), []);
  const closeSwitch = useCallback(() => setSwitchDialog((current) => ({ ...current, open: false })), []);

  return {
    closeLock,
    closeSwitch,
    loadStatus,
    lock,
    lockDialog,
    openSwitch,
    requestLock,
    setSwitchDialog,
    status,
    switchDatabase,
    switchDialog,
  };
}
