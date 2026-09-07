import { TriangleAlert } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Notice } from "../../../components/ui/notice";

export function KubernetesRestartDialog({ restart, styles }) {
  const value = restart.dialog;
  return (
    <Dialog open={value.open} onClose={restart.close} title="Rollout restart deployment" size="md" closeDisabled={value.pending}>
      <div className="grid gap-4">
        <Notice tone="warn"><TriangleAlert className="mr-2 inline h-4 w-4" />This restarts pods for the selected deployment. Keep this action in Prompt mode unless the workflow is trusted.</Notice>
        <div className={`rounded-md border p-3 text-sm ${styles.border}`}><p><span className={styles.muted}>Namespace:</span> {value.workload?.namespace}</p><p><span className={styles.muted}>Deployment:</span> {value.workload?.name}</p></div>
        <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={restart.close} disabled={value.pending}>Cancel</Button><Button type="button" onClick={restart.confirm} disabled={value.pending}>{value.pending ? "Restarting..." : "Rollout restart"}</Button></div>
      </div>
    </Dialog>
  );
}
