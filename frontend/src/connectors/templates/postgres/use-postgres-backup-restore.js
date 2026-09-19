import { useEffect, useState } from "react";
import { apiDownload, apiPostForm, currentWorkspaceBinding } from "../../../lib/api";
import { APIError, errorMessage } from "../../../lib/errors";
import {
  completeLocalActionRetry,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
} from "../../../lib/local-action-retry";
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
    let retry = null;
    setRestoreState({ state: "running", error: "", message: "" });
    try {
      const formData = new FormData();
      const workspaceID = currentWorkspaceBinding();
      retry = await prepareLocalActionRetry(
        {
          path: `${endpoint}/restore`,
          body: {
            confirm_target: capturedConfirmation,
            filename: capturedFile.name,
            size: capturedFile.size,
            last_modified: capturedFile.lastModified || 0,
          },
        },
        { workspaceID },
      );
      formData.append("dump", capturedFile);
      formData.append("confirm_target", capturedConfirmation);
      formData.append("idempotency_key", retry.idempotencyKey);
      const response = await apiPostForm(`${endpoint}/restore`, formData, {
        signal: request.signal,
        requireJSON: true,
        workspaceBinding: workspaceID,
      });
      requireCompletedRestoreResponse(response);
      await completeLocalActionRetry(retry);
      if (!request.isCurrent()) return;
      setRestoreState({ state: "ready", error: "", message: "Restore completed." });
      setFile(null);
      setConfirmTarget("");
    } catch (error) {
      let presentedError = error;
      if (retry) {
        try {
          await settleRestoreRetryFailure(retry, error);
        } catch (ledgerError) {
          presentedError = new Error(
            `The restore result could not be recorded locally. Inspect the database before retrying. ${errorMessage(ledgerError, "")}`.trim(),
          );
        }
      }
      if (request.isCurrent())
        setRestoreState({ state: "error", error: errorMessage(presentedError, "Could not restore backup."), message: "" });
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

const uncertainRestoreCodes = new Set(["audit_persistence_failed", "result_projection_failed"]);

export function requireCompletedRestoreResponse(response) {
  const acknowledged =
    response !== null &&
    typeof response === "object" &&
    Number.isSafeInteger(response.operation_id) &&
    response.operation_id > 0 &&
    response.status === "completed" &&
    (response.replayed === true || Object.hasOwn(response, "result"));
  if (acknowledged) return response;
  throw new APIError("Invalid restore completion response from gateway.", {
    code: "invalid_restore_response",
    data: { status: "outcome_unknown" },
  });
}

export async function settleRestoreRetryFailure(retry, error) {
  if (error instanceof APIError && (error.data?.status === "outcome_unknown" || uncertainRestoreCodes.has(error.code))) {
    await markLocalActionRetryOutcome(retry, error.data || { status: "outcome_unknown", code: error.code });
    return;
  }
  if (error instanceof APIError && ["failed", "canceled"].includes(error.data?.status)) {
    await completeLocalActionRetry(retry);
    return;
  }
  if (error instanceof APIError && error.status >= 400 && error.status < 500 && error.code !== "restore_in_progress") {
    await completeLocalActionRetry(retry);
    return;
  }
  await preserveLocalActionRetryAttempt(retry);
}
