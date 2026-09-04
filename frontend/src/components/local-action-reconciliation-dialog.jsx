import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Notice } from "./ui/notice";

export function LocalActionReconciliationDialog({ value, onClose }) {
  return (
    <Dialog
      open={Boolean(value)}
      title="Start a new external attempt?"
      description="The previous action has an unknown remote outcome."
      onClose={() => onClose(false)}
      size="md"
      closeOnOverlay={false}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          Inspect the connector target first. Continuing retires the protected retry key and may repeat an operation that already completed.
        </Notice>
        {value?.requestID ? <p className="text-sm text-stone-600">Request {value.requestID}</p> : null}
        {value?.assistantHint ? <p className="text-sm text-stone-600">{value.assistantHint}</p> : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => onClose(false)}>
            Keep protected
          </Button>
          <Button type="button" variant="danger" onClick={() => onClose(true)}>
            Start new attempt
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
