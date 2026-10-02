import { useEffect, useRef, useState } from "react";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { defaultS3ConfirmDialog, type S3ConfirmDialogState } from "./dialogs";

type S3ObjectDeleteOptions = {
  scopeKey: string;
  selectedKey: string;
  selectedETag?: string | null;
  trustConditionalRequests?: boolean;
  runAction: (_request: {
    actionName: "delete_object";
    input: { key: string; expected_etag?: string };
    reason: string;
    busy: string;
  }) => Promise<object | null>;
  clearSelection: () => void;
  refreshObjects: (_options: { reset: boolean }) => Promise<unknown>;
};

export function useS3ObjectDelete({
  scopeKey,
  selectedKey,
  selectedETag,
  trustConditionalRequests = false,
  runAction,
  clearSelection,
  refreshObjects,
}: S3ObjectDeleteOptions) {
  const [confirmDialog, setConfirmDialog] = useState<S3ConfirmDialogState>(defaultS3ConfirmDialog);
  const guard = useRequestGuard(scopeKey);
  const confirmation = useRef<{
    request: ReturnType<typeof guard.begin>;
    action: () => Promise<boolean>;
    pending: boolean;
  } | null>(null);

  useEffect(() => {
    confirmation.current = null;
    setConfirmDialog(defaultS3ConfirmDialog);
  }, [scopeKey]);

  function requestDelete() {
    if (!selectedKey) return;
    const objectKey = selectedKey;
    const expectedETag = trustConditionalRequests ? String(selectedETag || "").trim() : "";
    const request = guard.begin("confirmation");
    const action: () => Promise<boolean> = async () => {
      if (!request.isCurrent() || confirmation.current?.action !== action) return false;
      const deleted = await runAction({
        actionName: "delete_object",
        input: { key: objectKey, ...(expectedETag ? { expected_etag: expectedETag } : {}) },
        reason: "manual S3 browser object delete",
        busy: "deleting",
      });
      if (!request.isCurrent() || !deleted) return false;
      confirmation.current = null;
      setConfirmDialog((current) => ({
        ...current,
        title: "S3 object deleted",
        description: "The object deletion completed. The listing is being refreshed.",
        action: null,
        danger: false,
        status: "Deletion completed; refresh the listing before continuing if its reload fails.",
      }));
      clearSelection();
      await refreshObjects({ reset: true });
      return request.isCurrent();
    };
    confirmation.current = { request, action, pending: false };
    setConfirmDialog({
      open: true,
      title: "Delete S3 object",
      description: "This permanently deletes the selected object from the bucket.",
      details: [
        { label: "Object", value: JSON.stringify(objectKey) },
        ...(expectedETag ? [{ label: "Expected ETag", value: expectedETag }] : []),
      ],
      danger: true,
      pending: false,
      error: "",
      status: "",
      action,
    });
  }

  async function confirmPendingAction() {
    const attempt = confirmation.current;
    if (!attempt || attempt.action !== confirmDialog.action || attempt.pending || !attempt.request.isCurrent()) return;
    attempt.pending = true;
    setConfirmDialog((current) => ({ ...current, pending: true, error: "", status: "" }));
    try {
      const completed = await attempt.action();
      if (!attempt.request.isCurrent()) return;
      if (completed === false) {
        setConfirmDialog((current) => ({
          ...current,
          pending: false,
          status: "Approval or completion is pending. Review the activity before retrying.",
        }));
        return;
      }
      attempt.pending = false;
      closeConfirmDialog();
    } catch (error) {
      if (!attempt.request.isCurrent()) return;
      setConfirmDialog((current) => ({ ...current, pending: false, error: errorMessage(error, "S3 object deletion failed.") }));
    } finally {
      attempt.pending = false;
      attempt.request.complete();
    }
  }

  function closeConfirmDialog() {
    if (confirmation.current?.pending) return;
    guard.invalidate("confirmation");
    confirmation.current = null;
    setConfirmDialog(defaultS3ConfirmDialog);
  }

  return {
    confirmDialog,
    requestDelete,
    confirmPendingAction,
    closeConfirmDialog,
  };
}
