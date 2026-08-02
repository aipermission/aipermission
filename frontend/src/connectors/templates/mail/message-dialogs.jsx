import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { mailFolderEqual } from "./helpers";

export function MoveMessageDialog({ dialog, folders, busy, onClose, onConfirm, onDestination }) {
  return (
    <Dialog open={dialog.open} title="Move message" description="Move this exact IMAP UID to an allowed existing folder." onClose={onClose} closeDisabled={busy} closeOnOverlay={false} size="md">
      <div className="grid gap-4">
        <Field>
          Destination folder
          <Select value={dialog.destination} onChange={(event) => onDestination(event.target.value)}>
            <option value="" disabled>Select destination</option>
            {folders.filter((folder) => !mailFolderEqual(folder.name, dialog.sourceFolder)).map((folder) => <option value={folder.name} key={folder.name}>{folder.display_name || folder.name}</option>)}
          </Select>
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="button" onClick={onConfirm} disabled={busy || !dialog.destination}>{busy ? "Moving..." : "Move message"}</Button>
        </div>
      </div>
    </Dialog>
  );
}

export function DeleteMessageDialog({ open, trashFolder, busy, onClose, onConfirm }) {
  return (
    <Dialog open={open} title="Move message to Trash" description="This does not permanently expunge the message." onClose={onClose} closeDisabled={busy} closeOnOverlay={false} size="md">
      <div className="grid gap-4">
        <Notice tone="warn">The selected message will move to {trashFolder || "the configured Trash folder"}. AIPermission never performs permanent IMAP expunge.</Notice>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="button" variant="danger" onClick={onConfirm} disabled={busy}>{busy ? "Moving..." : "Move to Trash"}</Button>
        </div>
      </div>
    </Dialog>
  );
}

export function RetryUnknownSubmissionDialog({ value, busy, onClose, onConfirm }) {
  return (
    <Dialog open={Boolean(value?.open)} title="Retry an unknown SMTP submission?" description="The server may already have accepted the previous message." onClose={onClose} closeDisabled={busy} closeOnOverlay={false} size="md">
      <div className="grid gap-4">
        <Notice tone="bad">
          Inspect the Sent folder or server state first. Retrying can deliver a duplicate message.
          {value?.draftChanged ? <span className="mt-2 block">The draft changed after the unknown result, but the earlier message may still have been delivered.</span> : null}
          {value?.messageID ? <span className="mt-2 block break-all font-mono text-xs">Message-ID: {value.messageID}</span> : null}
        </Notice>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="button" variant="danger" onClick={onConfirm} disabled={busy}>{busy ? "Retrying..." : "Retry anyway"}</Button>
        </div>
      </div>
    </Dialog>
  );
}
