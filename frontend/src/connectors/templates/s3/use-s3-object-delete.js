import { useEffect, useState } from "react";
import { errorMessage } from "../../../lib/errors";
import { defaultS3ConfirmDialog } from "./dialogs";

export function useS3ObjectDelete({
  scopeKey,
  selectedKey,
  selectedETag,
  trustConditionalRequests = false,
  runAction,
  clearSelection,
  refreshObjects,
}) {
  const [confirmDialog, setConfirmDialog] = useState(defaultS3ConfirmDialog);

  useEffect(() => setConfirmDialog(defaultS3ConfirmDialog), [scopeKey]);

  function requestDelete() {
    if (!selectedKey) return;
    const objectKey = selectedKey;
    const expectedETag = trustConditionalRequests ? String(selectedETag || "").trim() : "";
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
      action: async () => {
        const deleted = await runAction({
          actionName: "delete_object",
          input: { key: objectKey, ...(expectedETag ? { expected_etag: expectedETag } : {}) },
          reason: "manual S3 browser object delete",
          busy: "deleting",
        });
        if (!deleted) return false;
        clearSelection();
        await refreshObjects({ reset: true });
        return true;
      },
    });
  }

  async function confirmPendingAction() {
    if (!confirmDialog.action || confirmDialog.pending) return;
    setConfirmDialog((current) => ({ ...current, pending: true, error: "", status: "" }));
    try {
      const completed = await confirmDialog.action();
      if (completed === false) {
        setConfirmDialog((current) => ({
          ...current,
          pending: false,
          status: "Approval or completion is pending. Review the activity before retrying.",
        }));
        return;
      }
      setConfirmDialog(defaultS3ConfirmDialog);
    } catch (error) {
      setConfirmDialog((current) => ({ ...current, pending: false, error: errorMessage(error, "S3 object deletion failed.") }));
    }
  }

  return {
    confirmDialog,
    requestDelete,
    confirmPendingAction,
    closeConfirmDialog: () => setConfirmDialog((current) => (current.pending ? current : defaultS3ConfirmDialog)),
  };
}
