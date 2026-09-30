import { ShieldAlert, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import {
  listLocalActionRetryEntries,
  localActionRetryLedgerChangedEvent,
  localActionRetryObservationFailedEvent,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "../../lib/local-action-retry";
import type { RetryListEntry } from "../../lib/local-action-retry.ts";
import { errorMessage } from "../../lib/errors.ts";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";
import { ServerRequestReconciliation } from "./server-request-reconciliation";

export function LocalActionRetryPanel() {
  const [entries, setEntries] = useState<RetryListEntry[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<RetryListEntry | { invalid: true } | null>(null);

  useEffect(() => {
    let active = true;
    let generation = 0;
    const refresh = async () => {
      const currentGeneration = ++generation;
      try {
        const next = await listLocalActionRetryEntries();
        if (!active || currentGeneration !== generation) return;
        setEntries(next);
        setError("");
      } catch (loadError) {
        if (!active || currentGeneration !== generation) return;
        setEntries([]);
        setError(errorMessage(loadError, "Could not load unresolved local actions."));
      } finally {
        if (active && currentGeneration === generation) setLoading(false);
      }
    };
    refresh();
    window.addEventListener(localActionRetryLedgerChangedEvent, refresh);
    window.addEventListener(localActionRetryObservationFailedEvent, refresh);
    return () => {
      active = false;
      window.removeEventListener(localActionRetryLedgerChangedEvent, refresh);
      window.removeEventListener(localActionRetryObservationFailedEvent, refresh);
    };
  }, []);

  async function resolveSelected() {
    try {
      if (selected?.invalid) await resetLocalActionRetryLedger();
      else if (selected) {
        const resolved = await resolveLocalActionRetryEntry(selected);
        if (!resolved) throw new Error("The retry identity changed in another tab. The list has been refreshed.");
      }
      setSelected(null);
    } catch (resolveError) {
      setError(errorMessage(resolveError, "Could not reconcile the local retry identity."));
    }
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Unresolved local actions</CardTitle>
          <CardDescription>Retry identities retained after interrupted or uncertain connector requests.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3">
          {loading ? (
            <Notice>Loading unresolved local actions...</Notice>
          ) : error ? (
            <div className="flex items-center justify-between gap-3">
              <Notice tone="bad">{error}</Notice>
              <Button type="button" variant="danger" onClick={() => setSelected({ invalid: true })}>
                Reset ledger
              </Button>
            </div>
          ) : entries.length === 0 ? (
            <Notice tone="good">No unresolved local connector attempts.</Notice>
          ) : (
            entries.map((entry) => (
              <div key={entry.signature} className="flex items-center justify-between gap-3 rounded-md border border-stone-200 p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-semibold text-stone-900">
                    <ShieldAlert className="h-4 w-4 text-amber-600" />
                    {entry.state === "outcome_unknown" ? "Outcome unknown" : "Request acknowledgement pending"}
                  </div>
                  <p className="mt-1 text-xs text-stone-500">
                    {retryReference(entry)}
                    {formatRetryTime(entry.updated_at)}
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  className="h-9 w-9 px-0"
                  title="Mark reconciled"
                  aria-label="Mark retry identity reconciled"
                  onClick={() => setSelected(entry)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))
          )}
          <ServerRequestReconciliation />
        </CardContent>
      </Card>
      <Dialog
        open={Boolean(selected)}
        title="Mark request reconciled?"
        description={
          selected?.invalid
            ? "This removes malformed local retry state that cannot be inspected."
            : "This removes the local retry identity only after you have inspected the external target."
        }
        onClose={() => setSelected(null)}
        closeOnOverlay={false}
        size="md"
      >
        <div className="grid gap-4">
          <Notice tone="warn">
            {selected?.invalid
              ? "Reset only if you understand that any protected local retry identities will be lost."
              : "The next identical action will be a new external attempt and may repeat a completed operation."}
          </Notice>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setSelected(null)}>
              Cancel
            </Button>
            <Button type="button" variant="danger" onClick={resolveSelected}>
              {selected?.invalid ? "Reset ledger" : "Mark reconciled"}
            </Button>
          </div>
        </div>
      </Dialog>
    </>
  );
}

function retryReference(entry: RetryListEntry) {
  if ("request_id" in entry && typeof entry.request_id === "number" && entry.request_id > 0) return `Request ${entry.request_id} · `;
  const operationRef = "operation_ref" in entry && typeof entry.operation_ref === "string" ? entry.operation_ref : "";
  if (operationRef.startsWith("operation:")) return `Operation ${operationRef.slice("operation:".length)} · `;
  if (operationRef) return `${operationRef} · `;
  return "";
}

function formatRetryTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Time unavailable" : date.toLocaleString();
}
