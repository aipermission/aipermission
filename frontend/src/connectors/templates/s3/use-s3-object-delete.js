import { useEffect, useState } from "react";
import { defaultS3ConfirmDialog } from "./dialogs";

export function useS3ObjectDelete({ scopeKey, selectedKey, runAction, clearSelection, refreshObjects }) {
  const [confirmDialog, setConfirmDialog] = useState(defaultS3ConfirmDialog);

  useEffect(() => setConfirmDialog(defaultS3ConfirmDialog), [scopeKey]);

  function requestDelete() {
    if (!selectedKey) return;
    const objectKey = selectedKey;
    setConfirmDialog({
      open: true,
      title: "Delete S3 object",
      description: "This permanently deletes the selected object from the bucket.",
      details: [{ label: "Object", value: JSON.stringify(objectKey) }],
      danger: true,
      pending: false,
      action: async () => {
        const deleted = await runAction({
          actionName: "delete_object",
          input: { key: objectKey },
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
    if (!confirmDialog.action) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    try {
      const completed = await confirmDialog.action();
      if (completed === false) {
        setConfirmDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setConfirmDialog(defaultS3ConfirmDialog);
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  return {
    confirmDialog,
    requestDelete,
    confirmPendingAction,
    closeConfirmDialog: () => setConfirmDialog(defaultS3ConfirmDialog),
  };
}
