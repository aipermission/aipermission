import type { ProvisionResult } from "./operation-types";

export function provisionResultResponse(value: unknown): ProvisionResult {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid Postgres provisioning response.");
  const data = value as Record<string, unknown>;
  if (data.profile !== undefined) {
    const profile = record(data.profile);
    if ((profile.id !== undefined && (typeof profile.id !== "number" || !Number.isSafeInteger(profile.id) || profile.id < 1)) ||
        (profile.label !== undefined && typeof profile.label !== "string")) throw new Error("Invalid Postgres provisioning profile.");
  }
  if (data.result !== undefined) {
    const result = record(data.result);
    if (result.display_text !== undefined && typeof result.display_text !== "string") throw new Error("Invalid Postgres provisioning result.");
  }
  return data as ProvisionResult;
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid Postgres provisioning response.");
  return value as Record<string, unknown>;
}
