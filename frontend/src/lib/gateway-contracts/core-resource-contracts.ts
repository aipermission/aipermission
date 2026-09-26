import type { components } from "../../../types/generated-openapi";

export type GatewayTarget = components["schemas"]["TargetProfile"] & { id?: number; name?: string };
export type GatewayToken = {
  id: number;
  name: string;
  token?: string;
  revoked_at?: string;
  expires_at?: string;
  created_at?: string;
  updated_at?: string;
};
export type CreatedGatewayToken = GatewayToken & { token: string };

export function gatewayCreatedTokenResponse(value: unknown): CreatedGatewayToken {
  if (!validToken(value) || typeof value.token !== "string" || !value.token) throw new Error("Invalid created token response.");
  return { ...value, token: value.token };
}
export type GatewayStatus = { status: string; service?: string; config?: Record<string, unknown>; features?: string[]; audit?: unknown };
export type MCPRuntime = { enabled: boolean; start_enabled: boolean; updated_at?: string };
export type CredentialResource = {
  id?: number | string;
  name?: string;
  kind?: string;
  resource_kind?: string;
  resource_ref?: string;
  connector_kind?: string;
  [field: string]: unknown;
};

export function gatewayTargetsResponse(value: unknown): GatewayTarget[] {
  if (!objectRecord(value) || !Array.isArray(value.items) || !value.items.every(validTarget))
    throw new Error("Invalid gateway targets response.");
  return value.items;
}

export function gatewayTokensResponse(value: unknown): GatewayToken[] {
  if (!Array.isArray(value) || !value.every(validToken)) throw new Error("Invalid gateway tokens response.");
  return value;
}

export function gatewayStatusResponse(value: unknown): GatewayStatus {
  if (
    !objectRecord(value) ||
    typeof value.status !== "string" ||
    !optionalStrings(value, ["service"]) ||
    (value.config !== undefined && !objectRecord(value.config)) ||
    (value.features !== undefined && (!Array.isArray(value.features) || !value.features.every((item: unknown) => typeof item === "string")))
  ) {
    throw new Error("Invalid gateway status response.");
  }
  return {
    status: value.status,
    service: typeof value.service === "string" ? value.service : undefined,
    config: objectRecord(value.config) ? value.config : undefined,
    features:
      Array.isArray(value.features) && value.features.every((item: unknown): item is string => typeof item === "string")
        ? value.features
        : undefined,
    audit: value.audit,
  };
}

export function mcpRuntimeResponse(value: unknown): MCPRuntime {
  if (
    !objectRecord(value) ||
    typeof value.enabled !== "boolean" ||
    typeof value.start_enabled !== "boolean" ||
    !optionalStrings(value, ["updated_at"])
  ) {
    throw new Error("Invalid MCP runtime response.");
  }
  return {
    enabled: value.enabled,
    start_enabled: value.start_enabled,
    ...(typeof value.updated_at === "string" ? { updated_at: value.updated_at } : {}),
  };
}

export function credentialResourcesResponse(value: unknown): CredentialResource[] {
  if (!Array.isArray(value) || !value.every(validCredentialResource)) throw new Error("Invalid connector credential resources.");
  return value;
}

function validTarget(value: unknown): value is GatewayTarget {
  if (!objectRecord(value)) return false;
  return (
    ["target_id", "profile_id", "project_id"].every((key) => positiveID(value[key])) &&
    [
      "connector_kind",
      "ref",
      "target_name",
      "profile_kind",
      "profile_label",
      "project_name",
      "project_slug",
      "status",
      "created_at",
      "updated_at",
    ].every((key) => typeof value[key] === "string") &&
    ["runtime_id", "transfer_runtime_id", "id"].every((key) => value[key] === undefined || positiveID(value[key])) &&
    ["config", "public"].every((key) => value[key] === undefined || objectRecord(value[key])) &&
    optionalStrings(value, ["name"])
  );
}

function validToken(value: unknown): value is GatewayToken {
  return (
    objectRecord(value) &&
    positiveID(value.id) &&
    typeof value.name === "string" &&
    optionalStrings(value, ["token", "revoked_at", "expires_at", "created_at", "updated_at"])
  );
}

function validCredentialResource(value: unknown): value is CredentialResource {
  return (
    objectRecord(value) &&
    (value.id === undefined || positiveID(value.id) || typeof value.id === "string") &&
    optionalStrings(value, ["name", "kind", "resource_kind", "resource_ref", "connector_kind"])
  );
}

function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function objectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function optionalStrings(value: Record<string, unknown>, fields: string[]) {
  return fields.every((key) => value[key] === undefined || typeof value[key] === "string");
}
