import { ChevronLeft, ChevronRight, ChevronsLeft, RefreshCcw } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Dialog } from "../../../../components/ui/dialog";
import { Notice } from "../../../../components/ui/notice";
import { useRoleHistory } from "./use-role-history";
import { canCleanupRole, canReconcileRole } from "./role-reconciliation-contract";
import { RoleReconciliationForm } from "./role-reconciliation-form";
import type { DatabaseProfile } from "../../_shared/database-model-types";
import type { RoleHistoryEntry } from "./role-history-types";

export function RoleHistoryDialog({
  target,
  onClose,
  workspaceBinding,
}: {
  target: { id: number; name?: string; profiles?: DatabaseProfile[] };
  onClose: () => void;
  workspaceBinding?: string;
}) {
  const history = useRoleHistory(target.id, workspaceBinding);
  return (
    <Dialog
      open
      title={<span className="break-all">{target.name || `Target ${target.id}`} role history</span>}
      onClose={onClose}
      closeDisabled={history.busy}
      size="lg"
      className="h-[min(760px,calc(100dvh-32px))] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-auto"
    >
      <div className="grid min-w-0 gap-3">
        <div className="flex items-center justify-between gap-2">
          <span className="text-sm text-stone-500">Page {history.pageNumber}</span>
          <Button
            type="button"
            variant="outline"
            className="h-8 w-8 px-0"
            title="Refresh role history"
            aria-label="Refresh role history"
            disabled={history.busy || history.state === "loading" || history.workspaceChanged}
            onClick={() => void history.refresh()}
          >
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </div>
        {history.state === "loading" ? <Notice>Loading role history...</Notice> : null}
        {history.busy ? <Notice>Verifying role decision...</Notice> : null}
        {history.notice ? <Notice>{history.notice}</Notice> : null}
        {history.state === "error" ? <Notice tone={history.workspaceChanged ? "warn" : "bad"}>{history.error}</Notice> : null}
        {history.state === "ready" && history.page?.entries.length === 0 ? <Notice>No role history.</Notice> : null}
        {history.page?.entries.some((entry) => canReconcileRole(entry) || canCleanupRole(entry)) && !target.profiles?.length ? (
          <Notice tone="warn">No admin profile is available. Role evidence is read-only.</Notice>
        ) : null}
        {history.page ? (
          <ul className="min-w-0 divide-y divide-stone-200" aria-label="Role history">
            {history.page.entries.map((entry) => (
              <RoleHistoryRow
                key={`${entry.resource_id}:${entry.record.generation}`}
                entry={entry}
                profiles={target.profiles || []}
                disabled={history.busy || history.workspaceChanged}
                onConfirm={canCleanupRole(entry) ? history.cleanup : history.reconcile}
              />
            ))}
          </ul>
        ) : null}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              className="h-8 w-8 px-0"
              title="First"
              aria-label="First"
              disabled={!history.canFirst}
              onClick={() => void history.first()}
            >
              <ChevronsLeft className="h-4 w-4" />
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-8 w-8 px-0"
              title="Previous"
              aria-label="Previous"
              disabled={!history.canPrevious}
              onClick={() => void history.previous()}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-8 w-8 px-0"
              title="Next"
              aria-label="Next"
              disabled={!history.canNext}
              onClick={() => void history.next()}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
          <Button type="button" variant="outline" disabled={history.busy} onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function RoleHistoryRow({
  entry,
  profiles,
  disabled,
  onConfirm,
}: {
  entry: RoleHistoryEntry;
  profiles: DatabaseProfile[];
  disabled: boolean;
  onConfirm: (_entry: RoleHistoryEntry, _profileID: number, _confirmedRoleName?: string) => Promise<void>;
}) {
  const { record, resource_id: resourceID } = entry;
  const { anchor, role_name: roleName } = record.intent;
  return (
    <li className="min-w-0 py-3 text-xs">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="min-w-0 break-all text-sm font-semibold text-stone-900">{roleName}</span>
        <span className="text-stone-600">{record.status.replaceAll("_", " ")}</span>
      </div>
      <dl className="mt-2 grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-stone-600 [&>dd]:break-all">
        <dt>Cluster</dt>
        <dd>{anchor.cluster_id}</dd>
        <dt>Database</dt>
        <dd>
          {anchor.database_name} (OID {anchor.database_oid})
        </dd>
        <dt>Successor</dt>
        <dd>
          {anchor.successor_name} (OID {anchor.successor_oid})
        </dd>
        <dt>Role OID</dt>
        <dd>{record.role_oid}</dd>
        <dt>Resource ID</dt>
        <dd>{resourceID}</dd>
        <dt>Generation</dt>
        <dd className="font-mono">{record.generation}</dd>
      </dl>
      {(canReconcileRole(entry) || canCleanupRole(entry)) && profiles.length > 0 ? (
        <RoleReconciliationForm entry={entry} profiles={profiles} disabled={disabled} onConfirm={onConfirm} />
      ) : null}
    </li>
  );
}
