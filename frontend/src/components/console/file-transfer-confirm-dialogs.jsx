import { Download } from "lucide-react";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";

export function ClearDownloadDialog({ open, onCancel, onContinue, onSave }) {
  return (
    <Dialog
      open={open}
      title="Clear unsaved download?"
      description="The remote download finished, but the browser save was not completed."
      onClose={onCancel}
      size="md"
      autoFocusClose={false}
      closeOnOverlay={false}
      closeOnEscape={false}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          If you clear now, the staged download will be removed from this transfer panel. Save it first if you still need the file.
        </Notice>
        <div className="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="button" variant="outline" onClick={onContinue}>
            Clear anyway
          </Button>
          <Button type="button" onClick={onSave}>
            <Download className="h-4 w-4" />
            Save
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

export function UnsavedDownloadCloseDialog({ open, onCancel, onCloseAnyway, onSave }) {
  return (
    <Dialog
      open={open}
      title="Close without saving download?"
      description="The remote download finished, but the browser save was not completed."
      onClose={onCancel}
      size="md"
      autoFocusClose={false}
      closeOnOverlay={false}
      closeOnEscape={false}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          Save the download before closing if you still need the staged file.
        </Notice>
        <div className="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="button" variant="outline" onClick={onCloseAnyway}>
            Close anyway
          </Button>
          <Button type="button" onClick={onSave}>
            <Download className="h-4 w-4" />
            Save
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

export function OverwriteConfirmDialog({ open, conflicts, onCancel, onOverwrite }) {
  return (
    <Dialog
      open={open}
      title="Overwrite remote files?"
      description="Some files already exist at the selected remote destination."
      onClose={onCancel}
      size="md"
      autoFocusClose={false}
      closeOnOverlay={false}
      closeOnEscape={false}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          Existing files will be replaced. Review the paths before continuing.
        </Notice>
        <div className="max-h-56 overflow-auto rounded-md border border-amber-300 bg-amber-50 p-3 font-mono text-xs text-amber-950">
          {conflicts.map((item) => (
            <p key={item.remote_path} className="break-all py-1">{item.remote_path}</p>
          ))}
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="button" variant="danger" onClick={onOverwrite}>
            Overwrite all
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
