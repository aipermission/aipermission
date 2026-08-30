import { Ban } from "lucide-react";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";

export function TokenRevokeDialog({ token, actionState, error, onClose, onConfirm }) {
  const revoking = actionState === "revoking";
  return (
    <Dialog
      open={Boolean(token)}
      title="Revoke API token"
      description="This decision takes effect immediately and cannot be undone."
      onClose={onClose}
      closeDisabled={revoking}
      closeOnOverlay={false}
    >
      <div className="grid gap-4">
        <Notice tone="bad">
          MCP requests using <strong>{token?.name || "this token"}</strong> will be rejected, and active Vault authorization sessions for it
          will be invalidated.
        </Notice>
        {error ? <Notice tone="bad">{error}</Notice> : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={revoking}>
            Cancel
          </Button>
          <Button type="button" variant="danger" onClick={onConfirm} disabled={revoking}>
            <Ban className="h-4 w-4" />
            {revoking ? "Revoking..." : "Revoke token"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
