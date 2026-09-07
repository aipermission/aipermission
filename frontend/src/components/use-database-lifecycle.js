import { useCallback, useState } from "react";
import { apiGet, apiPost } from "../lib/api";

const initialSwitchDialog = { open: false, database_id: "", password: "", state: "idle", error: null };
const initialLockDialog = { open: false, state: "idle", error: null };

export function useDatabaseLifecycle({ disconnectAllConsoleSessions, pollIsCurrent }) {
  const [status, setStatus] = useState({ state: "loading", data: null, error: null });
  const [switchDialog, setSwitchDialog] = useState(initialSwitchDialog);
  const [lockDialog, setLockDialog] = useState(initialLockDialog);

  const loadStatus = useCallback(
    async (generation) => {
      try {
        const data = await apiGet("/api/unlock/status");
        if (!pollIsCurrent(generation)) return;
        setStatus({ state: "ready", data, error: null });
      } catch (error) {
        if (!pollIsCurrent(generation)) return;
        setStatus({ state: "error", data: null, error: error.message });
      }
    },
    [pollIsCurrent],
  );

  const lock = useCallback(
    async (scope) => {
      setLockDialog((current) => ({ ...current, state: "locking", error: null }));
      disconnectAllConsoleSessions();
      try {
        await apiPost("/api/lock", { scope });
        window.location.reload();
      } catch (error) {
        setLockDialog((current) => ({ ...current, state: "error", error: error.message }));
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
    async (event) => {
      event?.preventDefault?.();
      if (switchDialog.database_id === status.data?.database_id) {
        setSwitchDialog((current) => ({ ...current, open: false }));
        return;
      }
      setSwitchDialog((current) => ({ ...current, state: "switching", error: null }));
      try {
        disconnectAllConsoleSessions();
        await apiPost("/api/databases/switch", {
          database_id: switchDialog.database_id,
          password: switchDialog.password,
        });
        window.location.reload();
      } catch (error) {
        setSwitchDialog((current) => ({ ...current, state: "error", error: error.message }));
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
