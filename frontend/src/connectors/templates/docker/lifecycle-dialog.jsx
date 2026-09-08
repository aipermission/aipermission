import { Power } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";

const emptyDialog = { open: false, title: "", description: "", details: [], actionName: "", pending: false };

export function DockerLifecycleDialog({ dialog, onClose, onConfirm }) {
  return (
    <Dialog
      open={dialog.open}
      title={dialog.title}
      description={dialog.description}
      size="md"
      onClose={onClose}
      closeDisabled={dialog.pending}
    >
      <div className="grid gap-4">
        <div className="grid gap-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
          {dialog.details.map((detail) => (
            <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-3" key={detail.label}>
              <span className="font-semibold">{detail.label}</span>
              <span className="min-w-0 break-words font-mono text-xs">{detail.value || "-"}</span>
            </div>
          ))}
        </div>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={dialog.pending}>
            Cancel
          </Button>
          <Button type="button" onClick={onConfirm} disabled={dialog.pending}>
            <Power className="h-4 w-4" />
            {dialog.pending ? "Running..." : "Run action"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

export function emptyDockerLifecycleDialog() {
  return { ...emptyDialog };
}
