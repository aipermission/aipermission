import { RefreshCw, Search, ShieldAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { apiGet, currentWorkspaceBinding } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { connectorApproval, connectorApprovals } from "../../lib/gateway-contracts/security-contracts";
import type { ConnectorApproval } from "../../lib/gateway-contracts/security-contracts";
import { localActionRetryLedgerChangedEvent } from "../../lib/local-action-retry";
import { currentRetryScope } from "../../lib/local-action-retry/runtime";
import { reconciledRequests, reconcileVerifiedServerRequest, requestWasReconciled } from "../../lib/local-action-retry/reconciliations";
import { useRequestGuard } from "../../lib/request-guard";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Input } from "../ui/form";
import { Notice } from "../ui/notice";

export function ServerRequestReconciliation() {
  const workspaceID = currentWorkspaceBinding();
  const guard = useRequestGuard(workspaceID);
  const [listing, setListing] = useState({ workspaceID, items: [] as ConnectorApproval[], error: "" });
  const [selection, setSelection] = useState<{ workspaceID: string; item: ConnectorApproval } | null>(null);
  const [resolution, setResolution] = useState({ workspaceID, busy: false, error: "" });
  const items = listing.workspaceID === workspaceID ? listing.items : [];
  const loadError = listing.workspaceID === workspaceID ? listing.error : "";
  const selected = selection?.workspaceID === workspaceID ? selection.item : null;
  const busy = resolution.workspaceID === workspaceID && resolution.busy;
  const error = resolution.workspaceID === workspaceID ? resolution.error : "";
  const [refreshVersion, setRefreshVersion] = useState(0);
  const [lookup, setLookup] = useState({ workspaceID, value: "", requestID: null as number | null });
  const requestID = lookup.workspaceID === workspaceID ? lookup.requestID : null;

  useEffect(() => {
    const refresh = async () => {
      const request = guard.begin("list");
      try {
        if (!workspaceID) throw new Error("Database request identity is unavailable.");
        const response = await apiGet(
          requestID ? `/api/connector-action-approvals/${requestID}` : "/api/connector-action-approvals?status=outcome_unknown",
          {
            signal: request.signal,
            timeoutMs: 10000,
            workspaceBinding: workspaceID,
          },
        );
        const data = requestID ? [connectorApproval(response)] : connectorApprovals(response);
        if (requestID && data[0].id !== requestID) throw new Error("Server request identity changed; refresh before reconciling.");
        const proofs = await reconciledRequests(currentRetryScope(workspaceID));
        if (!request.isCurrent() || currentWorkspaceBinding() !== workspaceID) return;
        setListing({
          workspaceID,
          items: data.filter((item) => item.status === "outcome_unknown" && !requestWasReconciled(proofs, item)),
          error: "",
        });
      } catch (loadError) {
        if (request.isCurrent() && currentWorkspaceBinding() === workspaceID)
          setListing({ workspaceID, items: [], error: errorMessage(loadError, "Could not load unresolved server requests.") });
      } finally {
        request.complete();
      }
    };
    void refresh();
    window.addEventListener(localActionRetryLedgerChangedEvent, refresh);
    return () => {
      guard.invalidate("list");
      window.removeEventListener(localActionRetryLedgerChangedEvent, refresh);
    };
  }, [guard, workspaceID, refreshVersion, requestID]);

  const close = () => {
    if (busy) return;
    guard.invalidate("resolve");
    setSelection(null);
  };

  async function reconcileSelected() {
    if (!selected || busy) return;
    const request = guard.begin("resolve");
    setResolution({ workspaceID, busy: true, error: "" });
    try {
      if (currentWorkspaceBinding() !== workspaceID) throw new Error("Database request identity changed.");
      const item = connectorApproval(
        await apiGet(`/api/connector-action-approvals/${selected.id}`, {
          signal: request.signal,
          timeoutMs: 10000,
          workspaceBinding: workspaceID,
        }),
      );
      if (!request.isCurrent()) return;
      if (
        currentWorkspaceBinding() !== workspaceID ||
        item.id !== selected.id ||
        item.target_ref !== selected.target_ref ||
        item.action_name !== selected.action_name
      )
        throw new Error("Server request identity changed; refresh before reconciling.");
      await reconcileVerifiedServerRequest(currentRetryScope(workspaceID), item);
      if (request.isCurrent()) {
        setSelection(null);
        setResolution({ workspaceID, busy: false, error: "" });
      }
    } catch (resolveError) {
      if (request.isCurrent())
        setResolution({ workspaceID, busy: false, error: errorMessage(resolveError, "Could not reconcile the server request.") });
    } finally {
      if (request.isCurrent())
        setResolution((previous) => (previous.workspaceID === workspaceID ? { ...previous, busy: false } : previous));
      request.complete();
    }
  }

  return (
    <section className="grid min-w-0 gap-3 border-t border-stone-200 pt-3 dark:border-stone-700">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Unresolved server requests</h3>
        <Button
          type="button"
          variant="outline"
          className="h-9 w-9 px-0"
          title="Refresh server requests"
          aria-label="Refresh server requests"
          onClick={() => {
            setLookup({ workspaceID, value: "", requestID: null });
            setRefreshVersion((value) => value + 1);
          }}
        >
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>
      <form
        className="flex min-w-0 items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          const value = lookup.workspaceID === workspaceID ? lookup.value : "";
          const id = /^[1-9]\d*$/.test(value) ? Number(value) : 0;
          if (!Number.isSafeInteger(id) || id <= 0) {
            setListing({ workspaceID, items: [], error: "Enter a positive request ID." });
            return;
          }
          setLookup({ workspaceID, value, requestID: id });
          setRefreshVersion((version) => version + 1);
        }}
      >
        <Input
          aria-label="Server request ID"
          placeholder="Request ID"
          inputMode="numeric"
          value={lookup.workspaceID === workspaceID ? lookup.value : ""}
          onChange={(event) => setLookup({ workspaceID, value: event.target.value, requestID })}
        />
        <Button
          type="submit"
          variant="outline"
          className="h-9 w-9 shrink-0 px-0"
          title="Find server request"
          aria-label="Find server request"
        >
          <Search className="h-4 w-4" />
        </Button>
      </form>
      {loadError ? <Notice tone="bad">{loadError}</Notice> : null}
      <div className="grid max-h-64 gap-2 overflow-y-auto">
        {items.map((item) => (
          <div
            key={item.id}
            className="flex min-w-0 items-center justify-between gap-3 rounded-md border border-stone-200 p-3 dark:border-stone-700"
          >
            <div className="min-w-0 text-sm">
              <div className="font-semibold">Request {item.id}</div>
              <div className="break-words text-xs text-stone-500">
                {item.target_ref} · {item.action_name}
              </div>
            </div>
            <Button
              type="button"
              variant="outline"
              className="h-9 w-9 shrink-0 px-0"
              title="Reconcile server request"
              aria-label={`Reconcile server request ${item.id}`}
              onClick={() => {
                setSelection({ workspaceID, item });
                setResolution({ workspaceID, busy: false, error: "" });
              }}
            >
              <ShieldAlert className="h-4 w-4" />
            </Button>
          </div>
        ))}
      </div>
      <Dialog
        open={Boolean(selected)}
        title="Reconcile server request?"
        description={selected ? `Request ${selected.id} · ${selected.target_ref} · ${selected.action_name}` : ""}
        onClose={close}
        closeOnOverlay={false}
        size="md"
      >
        <div className="grid gap-4">
          {error ? <Notice tone="bad">{error}</Notice> : null}
          <Notice tone="warn">
            Confirm only after inspecting this exact request and external target. This records your local decision, not successful
            execution. Any unidentified local retry must be reconciled separately.
          </Notice>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={busy} onClick={close}>
              Cancel
            </Button>
            <Button type="button" variant="danger" disabled={busy} onClick={reconcileSelected}>
              {busy ? "Verifying request..." : "Reconcile server request"}
            </Button>
          </div>
        </div>
      </Dialog>
    </section>
  );
}
