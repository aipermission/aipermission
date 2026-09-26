import { useEffect, useRef, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import type { KubernetesResource, KubernetesResourceKind } from "./resource-types";
import type { useKubernetesBrowser } from "./use-kubernetes-browser";

type RestartDialog = { open: boolean; pending: boolean; workload: KubernetesResource | null };
const emptyRestart: RestartDialog = { open: false, pending: false, workload: null };

export interface RolloutRestartProps {
  targetRef: string;
  tab: KubernetesResourceKind;
  selectedResource: KubernetesResource | null;
  runAction: ReturnType<typeof useKubernetesBrowser>["runAction"];
  refreshResource: ReturnType<typeof useKubernetesBrowser>["refreshResource"];
}

export function useRolloutRestart({ targetRef, tab, selectedResource, runAction, refreshResource }: RolloutRestartProps) {
  const [dialog, setDialog] = useState(emptyRestart);
  const requests = useRequestGuard(`rollout-restart:${targetRef}`);
  const pendingRequest = useRef<ReturnType<typeof requests.begin> | null>(null);

  useEffect(() => {
    pendingRequest.current = null;
    setDialog(emptyRestart);
  }, [targetRef]);

  function open(workload = selectedResource) {
    if (!workload || tab !== "workloads" || workload.kind !== "Deployment" || pendingRequest.current) return;
    requests.invalidate("restart");
    setDialog({ open: true, pending: false, workload });
  }

  async function confirm() {
    const workload = dialog.workload;
    if (!workload || pendingRequest.current) return;
    const request = requests.begin("restart");
    pendingRequest.current = request;
    setDialog((current) => ({ ...current, pending: true }));
    try {
      const completed = await runAction({
        actionName: "rollout_restart",
        input: { namespace: workload.namespace, deployment: workload.name },
        reason: "manual Kubernetes browser rollout restart",
        busy: "writing",
        channel: "restart",
      });
      if (!request.isCurrent()) return;
      if (!completed) {
        setDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setDialog(emptyRestart);
      await refreshResource("workloads");
    } catch (error) {
      if (!request.isCurrent()) return;
      setDialog((current) => ({ ...current, pending: false }));
      throw error;
    } finally {
      if (pendingRequest.current === request) pendingRequest.current = null;
      request.complete();
    }
  }

  function close() {
    if (pendingRequest.current) return;
    requests.invalidate("restart");
    setDialog(emptyRestart);
  }

  return { dialog, open, confirm, close };
}
