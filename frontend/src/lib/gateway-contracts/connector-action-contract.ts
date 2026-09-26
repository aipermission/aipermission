import { connectorActionResponseRequiredFields, connectorActionStatuses, connectorRetryClasses } from "./generated-connector-contract.ts";
import type { components } from "../../../types/generated-openapi";

type ActionResponse = components["schemas"]["ConnectorActionResponse"];
type RetryPolicy = ActionResponse["retry_policy"];

const connectorActionStatusSet: ReadonlySet<string> = new Set(connectorActionStatuses);
const connectorRetryClassSet: ReadonlySet<string> = new Set(connectorRetryClasses);
const connectorActionResponseFields = new Set([
  "status",
  "request_id",
  "target_ref",
  "target_name",
  "connector_kind",
  "profile_label",
  "action_name",
  "input",
  "output",
  "display_text",
  "error",
  "retry_policy",
  "retry_after_seconds",
  "assistant_hint",
  "output_withheld",
  "replayed",
]);
const connectorRetryPolicyFields = new Set(["class", "guidance", "precondition_fields"]);

export function assertConnectorActionResponse(value: unknown, expected?: { targetRef: string; actionName: string }): ActionResponse {
  const item = record(value);
  if (
    !hasOnlyKeys(item, connectorActionResponseFields) ||
    !connectorActionResponseRequiredFields.every((field) => Object.hasOwn(item, field)) ||
    !isConnectorActionStatus(item.status) ||
    !positiveID(item.request_id) ||
    !nonEmptyString(item.target_ref) ||
    !nonEmptyString(item.connector_kind) ||
    !nonEmptyString(item.action_name) ||
    !isConnectorRetryPolicy(item.retry_policy) ||
    !optionalString(item.target_name) ||
    !optionalString(item.profile_label) ||
    !optionalRecord(item.input) ||
    (item.display_text !== undefined && typeof item.display_text !== "string") ||
    !optionalString(item.error) ||
    !optionalRetryAfterSeconds(item.retry_after_seconds) ||
    !optionalString(item.assistant_hint) ||
    !optionalBoolean(item.output_withheld) ||
    !optionalBoolean(item.replayed) ||
    (item.output_withheld === true && ["input", "output", "display_text", "error"].some((field) => Object.hasOwn(item, field))) ||
    (expected && (item.target_ref !== expected.targetRef || item.action_name !== expected.actionName))
  ) {
    throw new Error("Invalid connector action response from gateway.");
  }
  return item as ActionResponse;
}

export function isConnectorActionStatus(value: unknown): value is ActionResponse["status"] {
  return typeof value === "string" && connectorActionStatusSet.has(value);
}

export function isConnectorRetryPolicy(value: unknown): value is RetryPolicy {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const policy = value as Record<string, unknown>;
  return (
    hasOnlyKeys(policy, connectorRetryPolicyFields) &&
    typeof policy.class === "string" &&
    connectorRetryClassSet.has(policy.class) &&
    typeof policy.guidance === "string" &&
    (policy.precondition_fields === undefined ||
      (Array.isArray(policy.precondition_fields) && policy.precondition_fields.every((field: unknown) => typeof field === "string")))
  );
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: ReadonlySet<string>): boolean {
  return Object.keys(value).every((field) => allowed.has(field));
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid connector action response from gateway.");
  return value as Record<string, unknown>;
}

function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

function optionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function optionalRecord(value: unknown): boolean {
  return value === undefined || (!!value && typeof value === "object" && !Array.isArray(value));
}

function optionalRetryAfterSeconds(value: unknown): boolean {
  return value === undefined || (typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= 3600);
}

function optionalBoolean(value: unknown): boolean {
  return value === undefined || typeof value === "boolean";
}
