import { ChevronDown, Plus } from "lucide-react";
import { ActionMenu } from "../../components/ui/action-menu";
import { ConnectorIcon, connectorKindLabel, connectorSummary } from "../templates/common";

export function AddCredentialMenu({ kinds, onAdd }: { kinds: readonly string[]; onAdd: (_kind: string) => void }) {
  return (
    <ActionMenu
      trigger={
        <>
          <Plus className="h-4 w-4" />
          Add credential
          <ChevronDown className="h-4 w-4" />
        </>
      }
      items={kinds}
      renderItem={(kind) => (
        <span className="flex gap-3">
          <ConnectorIcon kind={kind} className="mt-0.5 h-4 w-4 shrink-0 text-emerald-900" />
          <span className="min-w-0">
            <span className="block font-semibold">{connectorKindLabel(kind)}</span>
            <span className="mt-1 block text-xs text-stone-500">{connectorSummary(kind)}</span>
          </span>
        </span>
      )}
      onSelect={onAdd}
      label="Credential connector types"
      panelClassName="w-[min(320px,calc(100vw-32px))]"
      empty={<div className="px-2 py-1 text-sm text-stone-500">No backend-supported connector templates are available.</div>}
    />
  );
}
