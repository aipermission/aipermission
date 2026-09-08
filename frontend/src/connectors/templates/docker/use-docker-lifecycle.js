import { useState } from "react";
import { emptyDockerLifecycleDialog } from "./lifecycle-dialog";

export function useDockerLifecycle({ selectedContainer, runAction, refreshContainers }) {
  const [confirmDialog, setConfirmDialog] = useState(emptyDockerLifecycleDialog);

  function openLifecycle(actionName) {
    if (!selectedContainer) return;
    const verb = actionName.replace("_container", "");
    setConfirmDialog({
      open: true,
      title: `${capitalize(verb)} Docker container`,
      description: `This will ${verb} the selected container through the Docker connector.`,
      details: [
        { label: "Container", value: selectedContainer.name || selectedContainer.id },
        { label: "Image", value: selectedContainer.image },
        { label: "Current status", value: selectedContainer.status },
      ],
      actionName,
      pending: false,
    });
  }

  async function confirmLifecycle() {
    if (!confirmDialog.actionName || !selectedContainer) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    const input = { container: selectedContainer.name || selectedContainer.id };
    if (confirmDialog.actionName === "stop_container" || confirmDialog.actionName === "restart_container") input.timeout_seconds = 10;
    try {
      const completed = await runAction({
        actionName: confirmDialog.actionName,
        input,
        reason: "manual Docker browser lifecycle action",
        busy: "writing",
        channel: "lifecycle",
      });
      if (!completed) {
        setConfirmDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setConfirmDialog(emptyDockerLifecycleDialog());
      await refreshContainers();
    } catch {
      setConfirmDialog((current) => ({ ...current, pending: false }));
    }
  }

  return {
    confirmDialog,
    openLifecycle,
    confirmLifecycle,
    closeConfirmDialog: () => setConfirmDialog(emptyDockerLifecycleDialog()),
    resetLifecycle: () => setConfirmDialog(emptyDockerLifecycleDialog()),
  };
}

function capitalize(value) {
  const text = String(value || "");
  return text ? text[0].toUpperCase() + text.slice(1) : text;
}
