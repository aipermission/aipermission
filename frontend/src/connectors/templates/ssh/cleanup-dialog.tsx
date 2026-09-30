import { RefreshCcw } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Notice } from "../../../components/ui/notice";
import { CleanupProofForm } from "./cleanup-proof-form";
import { useCleanupReconciliation } from "./use-cleanup-reconciliation";
import type { SSHModelTarget } from "./model-types";

export function SSHCleanupDialog({ target, onClose }: { target: SSHModelTarget; onClose: () => void }) {
  const state = useCleanupReconciliation(Number(target.id));
  const busy = state.phase === "submitting";
  return (
    <Dialog
      open
      title={`Reconcile key cleanup for ${target.name}`}
      onClose={onClose}
      closeDisabled={busy}
      size="lg"
      className="h-[min(760px,calc(100dvh-32px))] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="flex min-h-0 flex-col gap-4 overflow-hidden"
    >
      <div className="flex shrink-0 items-center justify-between gap-3">
        <span className="text-sm text-stone-500">Historical key cleanup evidence</span>
        <Button
          type="button"
          variant="outline"
          className="h-9 w-9 px-0"
          title="Reload cleanup evidence"
          disabled={busy || state.phase === "loading"}
          onClick={() => void state.refresh()}
        >
          <RefreshCcw className="h-4 w-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="grid gap-4">
          <Notice tone="warn">
            Verify every recorded location through an independent admin session or provider console. Do not use a potentially revoked key.
            Recording evidence does not remove a remote key.
          </Notice>
          {state.notice ? <Notice>{state.notice}</Notice> : null}
          {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
          {state.phase === "loading" ? (
            <div role="status" className="py-8 text-center text-sm text-stone-500">
              Loading cleanup evidence...
            </div>
          ) : null}
          {state.snapshot?.records.length === 0 ? (
            <div className="py-8 text-center text-sm text-stone-500">No recorded key cleanup for this target.</div>
          ) : null}
          {state.snapshot && state.snapshot.records.length > 0 ? (
            <CleanupProofForm key={state.revision} snapshot={state.snapshot} disabled={busy} onSubmit={state.submit} />
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}
