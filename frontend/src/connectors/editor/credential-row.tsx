import { Pencil, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { ConnectorIcon } from "../templates/common";

export type CredentialDisplayRow = {
  row_id: string;
  connector_kind: string;
  connector_label: string;
  name: string;
  kind: string;
  target_label: string;
  target_detail?: string;
  metadata: readonly string[];
  delete_disabled?: string;
};

export function CredentialRow<Row extends CredentialDisplayRow>({
  row,
  onEdit,
  onDelete,
  busy,
  operations,
}: {
  row: Row;
  onEdit: (_row: Row) => unknown;
  onDelete: (_row: Row) => unknown;
  busy: boolean;
  operations?: ReactNode;
}) {
  return (
    <tr className="align-top" key={row.row_id}>
      <td className="px-4 py-4">
        <div className="flex min-w-0 items-center gap-2">
          <ConnectorIcon kind={row.connector_kind} className="h-4 w-4 shrink-0 text-emerald-900" />
          <span className="truncate font-semibold">{row.connector_label}</span>
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1">
          <span className="truncate font-semibold">{row.name}</span>
          <Badge className="w-fit">{row.kind}</Badge>
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1">
          <span className="truncate text-sm text-stone-700">{row.target_label}</span>
          {row.target_detail ? <span className="truncate font-mono text-xs text-stone-500">{row.target_detail}</span> : null}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1 text-xs text-stone-500">
          {row.metadata.map((item) => (
            <span className="truncate" key={item}>
              {item}
            </span>
          ))}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">{operations ?? <span className="text-xs text-stone-400">None</span>}</div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Edit credential"
            onClick={() => onEdit(row)}
            disabled={busy}
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title={row.delete_disabled || "Delete credential"}
            onClick={() => onDelete(row)}
            disabled={Boolean(row.delete_disabled) || busy}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </td>
    </tr>
  );
}
