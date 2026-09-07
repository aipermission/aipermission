import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Notice } from "./ui/notice";

export function DatabaseLockDialog({ state, onClose, onLock }) {
  return (
    <Dialog
      open={state.open}
      title="Lock database"
      description="More than one database is currently unlocked. Choose what should be locked."
      onClose={onClose}
      size="md"
    >
      <div className="grid gap-4">
        <Notice>
          Lock current closes only the active database and switches to another unlocked database if one is available. Lock all closes every
          unlocked database and stops MCP access until a database is unlocked again.
        </Notice>
        {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <Button type="button" variant="outline" disabled={state.state === "locking"} onClick={() => onLock("current")}>
            Lock current
          </Button>
          <Button type="button" variant="danger" disabled={state.state === "locking"} onClick={() => onLock("all")}>
            Lock all
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
