import type { CredentialDisplayRow } from "../../editor/credential-row";

export function credentialDisplayRow(
  row: Omit<Partial<CredentialDisplayRow>, "metadata"> &
    Pick<CredentialDisplayRow, "row_id" | "connector_kind"> & { metadata?: readonly (string | undefined)[] },
): CredentialDisplayRow {
  return {
    row_id: row.row_id,
    connector_kind: row.connector_kind,
    connector_label: row.connector_label || "",
    name: row.name || "",
    kind: row.kind || "",
    target_label: row.target_label || "",
    target_detail: row.target_detail,
    metadata: (row.metadata || []).filter((item): item is string => typeof item === "string"),
    delete_disabled: row.delete_disabled,
  };
}
