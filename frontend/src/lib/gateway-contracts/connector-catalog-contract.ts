import { connectorActionMaximumInputBytes } from "./generated-connector-contract";

export type ConnectorCatalogItem = { kind: string; label: string; version: string };
export type ConnectorCatalogDetail = ConnectorCatalogItem & Record<string, unknown>;
export type ConnectorPermissionAction = {
  name: string;
  category?: string;
  risk?: string;
  description?: string;
  max_input_bytes?: number;
  [field: string]: unknown;
};

export function connectorActionsResponse(value: unknown): ConnectorPermissionAction[] {
  if (!objectRecord(value)) throw new Error("Invalid connector action catalog from gateway.");
  return connectorActionItems(value.items);
}

export function connectorActionItems(value: unknown): ConnectorPermissionAction[] {
  const invalid = () => new Error("Invalid connector action catalog from gateway.");
  if (!Array.isArray(value)) throw invalid();
  return value.map((entry) => {
    if (!objectRecord(entry) || typeof entry.name !== "string" || !entry.name) throw invalid();
    for (const field of ["category", "risk", "description"]) {
      if (entry[field] !== undefined && typeof entry[field] !== "string") throw invalid();
    }
    if (
      entry.max_input_bytes !== undefined &&
      (typeof entry.max_input_bytes !== "number" ||
        !Number.isSafeInteger(entry.max_input_bytes) ||
        entry.max_input_bytes < 1 ||
        entry.max_input_bytes > connectorActionMaximumInputBytes)
    )
      throw invalid();
    return entry as ConnectorPermissionAction;
  });
}

export function connectorCatalogResponse(value: unknown): ConnectorCatalogItem[] {
  if (!objectRecord(value) || !Array.isArray(value.items)) throw invalidCatalog();
  const items = value.items.map((item) => connectorCatalogDetailResponse(item));
  if (new Set(items.map((item) => item.kind)).size !== items.length) throw invalidCatalog();
  return items;
}

export function connectorCatalogDetailResponse(value: unknown, expectedKind?: string): ConnectorCatalogDetail {
  if (
    !objectRecord(value) ||
    typeof value.kind !== "string" ||
    !/^[a-z][a-z0-9_]*$/.test(value.kind) ||
    (expectedKind !== undefined && value.kind !== expectedKind) ||
    typeof value.label !== "string" ||
    !value.label.trim() ||
    typeof value.version !== "string" ||
    !value.version.trim()
  )
    throw invalidCatalog();
  return { ...value, kind: value.kind, label: value.label, version: value.version };
}

function objectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function invalidCatalog() {
  return new Error("Invalid connector catalog from gateway.");
}
