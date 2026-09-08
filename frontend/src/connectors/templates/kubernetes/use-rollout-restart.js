import { useEffect, useState } from "react";

const emptyRestart = { open: false, pending: false, workload: null };

export function useRolloutRestart({ targetRef, tab, selectedResource, runAction, refreshResource }) {
  const [dialog, setDialog] = useState(emptyRestart);

  useEffect(() => setDialog(emptyRestart), [targetRef]);

  function open(workload = selectedResource) {
    if (!workload || tab !== "workloads" || workload.kind !== "Deployment") return;
    setDialog({ open: true, pending: false, workload });
  }

  async function confirm() {
    const workload = dialog.workload;
    if (!workload) return;
    setDialog((current) => ({ ...current, pending: true }));
    const completed = await runAction({
      actionName: "rollout_restart",
      input: { namespace: workload.namespace, deployment: workload.name },
      reason: "manual Kubernetes browser rollout restart",
      busy: "writing",
      channel: "restart",
    });
    if (!completed) {
      setDialog((current) => ({ ...current, pending: false }));
      return;
    }
    setDialog(emptyRestart);
    await refreshResource("workloads");
  }

  return { dialog, open, confirm, close: () => setDialog(emptyRestart) };
}
