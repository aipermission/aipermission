import type { ReactNode } from "react";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { Notice } from "../../components/ui/notice";
import type { AsyncActionState } from "../../lib/use-async-action";
import type { CredentialDisplayRow } from "./credential-row";

export type CredentialDeleteDialogMetadata = {
  title?: string;
  description?: string;
  details?: readonly { label: string; value?: ReactNode }[];
  notice?: ReactNode;
  confirmLabel?: string;
};

export function defaultCredentialDeleteDialog(row: Pick<CredentialDisplayRow, "name" | "connector_label" | "target_label">) {
  return {
    title: `Delete ${row.name}`,
    description: "Remove this connector credential profile from aipermission.",
    details: [
      { label: "Connector", value: row.connector_label },
      { label: "Credential", value: row.name },
      { label: "Target", value: row.target_label },
    ],
    notice:
      "This removes the locally stored credential profile. Connector-provisioned credentials may perform connector-owned external cleanup first.",
    confirmLabel: "Delete credential",
  };
}

export function DeleteCredentialDialog({
  value,
  state,
  onClose,
  onDelete,
}: {
  value: { open: boolean; row: object | null; dialog: CredentialDeleteDialogMetadata | null; attempted: boolean };
  state: AsyncActionState;
  onClose: () => void;
  onDelete: () => unknown;
}) {
  const dialog = value.dialog;
  const deleting = state.state === "deleting";
  return (
    <Dialog
      open={value.open}
      title={dialog?.title || "Delete credential"}
      description={dialog?.description}
      onClose={onClose}
      closeDisabled={deleting}
      size="md"
    >
      {value.row ? (
        <div className="grid gap-4">
          <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
            {(dialog?.details || [])
              .filter((item) => item.value)
              .map((item) => (
                <p className="mt-1 first:mt-0" key={item.label}>
                  <span className="font-semibold">{item.label}: </span>
                  <span>{item.value}</span>
                </p>
              ))}
          </div>
          {dialog?.notice ? <Notice tone="warn">{dialog.notice}</Notice> : null}
          {value.attempted && state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={deleting}>
              Cancel
            </Button>
            <Button type="button" variant="danger" onClick={onDelete} disabled={deleting}>
              {deleting ? "Deleting..." : dialog?.confirmLabel || "Delete credential"}
            </Button>
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}
