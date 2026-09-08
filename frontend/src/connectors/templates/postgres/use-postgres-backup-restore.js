import { useEffect, useState } from "react";
import { apiDownload, apiPostForm } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { safeBackupFilename } from "./provisioning";

const emptyActionState = { state: "idle", error: "", message: "" };

export function usePostgresBackupRestore(value) {
  const [backupState, setBackupState] = useState(emptyActionState);
  const [restoreState, setRestoreState] = useState(emptyActionState);
  const [file, setFile] = useState(null);
  const [confirmTarget, setConfirmTarget] = useState("");
  const [activeTab, setActiveTab] = useState("backup");
  const targetID = value.target?.id;
  const profileID = value.profile?.id;
  const targetName = value.target?.name || "";
  const endpoint = targetID && profileID ? `/api/connector-targets/${targetID}/profiles/${profileID}` : "";
  const requestGuard = useRequestGuard(`${value.open ? "open" : "closed"}:${targetID || 0}:${profileID || 0}`);
  const isWorking = backupState.state === "running" || restoreState.state === "running";
  const restoreReady = Boolean(endpoint && file && confirmTarget === targetName && !isWorking);

  useEffect(() => {
    if (!value.open) return;
    setBackupState(emptyActionState);
    setRestoreState(emptyActionState);
    setFile(null);
    setConfirmTarget("");
    setActiveTab("backup");
  }, [value.open, targetID, profileID]);

  async function downloadBackup() {
    if (!endpoint) return;
    const request = requestGuard.begin("backup");
    setBackupState({ state: "running", error: "", message: "" });
    try {
      const result = await apiDownload(`${endpoint}/backup`, `${safeBackupFilename(targetName || "postgres")}.sql`, {
        picker: true,
        signal: request.signal,
      });
      if (!request.isCurrent()) return;
      setBackupState(
        result?.canceled ? emptyActionState : { state: "ready", error: "", message: "Backup downloaded as a restore-grade SQL dump." },
      );
    } catch (error) {
      if (request.isCurrent()) setBackupState({ state: "error", error: errorMessage(error, "Could not download backup."), message: "" });
    } finally {
      request.complete();
    }
  }

  async function restoreBackup(event) {
    event.preventDefault();
    if (!restoreReady) return;
    const request = requestGuard.begin("restore");
    const capturedFile = file;
    const capturedConfirmation = confirmTarget;
    setRestoreState({ state: "running", error: "", message: "" });
    try {
      const formData = new FormData();
      formData.append("dump", capturedFile);
      formData.append("confirm_target", capturedConfirmation);
      await apiPostForm(`${endpoint}/restore`, formData, { signal: request.signal });
      if (!request.isCurrent()) return;
      setRestoreState({ state: "ready", error: "", message: "Restore completed." });
      setFile(null);
      setConfirmTarget("");
    } catch (error) {
      if (request.isCurrent()) setRestoreState({ state: "error", error: errorMessage(error, "Could not restore backup."), message: "" });
    } finally {
      request.complete();
    }
  }

  return {
    activeTab,
    setActiveTab,
    backupState,
    restoreState,
    file,
    setFile,
    confirmTarget,
    setConfirmTarget,
    targetName,
    endpoint,
    isWorking,
    restoreReady,
    downloadBackup,
    restoreBackup,
  };
}
