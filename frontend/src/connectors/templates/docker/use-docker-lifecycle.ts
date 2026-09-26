import { useRef, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { emptyDockerLifecycleDialog } from "./lifecycle-dialog";
import type { DockerLifecycleAction } from "./lifecycle-dialog";
import type { DockerResource } from "./resource-types";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { DockerRunActionOptions } from "./action-types";

export function useDockerLifecycle({
  targetRef = "",
  selectedContainer,
  runAction,
  refreshContainers,
}: {
  targetRef?: string;
  selectedContainer: DockerResource | null;
  runAction: (_options: DockerRunActionOptions) => Promise<ConnectorActionResponse | null>;
  refreshContainers: () => Promise<void>;
}) {
  const [confirmDialog, setConfirmDialog] = useState(emptyDockerLifecycleDialog);
  const requestGuard = useRequestGuard(targetRef);
  const confirmedResource = useRef<{ container: string; actionName: DockerLifecycleAction } | null>(null);
  const pending = useRef(false);

  function resetLifecycle() {
    requestGuard.invalidate("confirmation");
    confirmedResource.current = null;
    pending.current = false;
    setConfirmDialog(emptyDockerLifecycleDialog());
  }

  function openLifecycle(actionName: DockerLifecycleAction) {
    if (!selectedContainer) return;
    requestGuard.invalidate("confirmation");
    pending.current = false;
    confirmedResource.current = { container: selectedContainer.name || selectedContainer.id || "", actionName };
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
    const confirmed = confirmedResource.current;
    if (!confirmed?.container || pending.current) return;
    pending.current = true;
    const request = requestGuard.begin("confirmation");
    setConfirmDialog((current) => ({ ...current, pending: true }));
    const input: Record<string, unknown> = { container: confirmed.container };
    if (confirmed.actionName === "stop_container" || confirmed.actionName === "restart_container") input.timeout_seconds = 10;
    try {
      const completed = await runAction({
        actionName: confirmed.actionName,
        input,
        reason: "manual Docker browser lifecycle action",
        busy: "writing",
        channel: "lifecycle",
      });
      if (!request.isCurrent()) return;
      if (!completed) {
        setConfirmDialog((current) => ({ ...current, pending: false }));
        return;
      }
      resetLifecycle();
      await refreshContainers();
    } catch {
      if (!request.isCurrent()) return;
      setConfirmDialog((current) => ({ ...current, pending: false }));
    } finally {
      if (request.isCurrent()) pending.current = false;
      request.complete();
    }
  }

  return {
    confirmDialog,
    openLifecycle,
    confirmLifecycle,
    closeConfirmDialog: resetLifecycle,
    resetLifecycle,
  };
}

function capitalize(value: string) {
  const text = String(value || "");
  return text ? text[0].toUpperCase() + text.slice(1) : text;
}
