import { connectorActionResponseRequiredFields, connectorActionStatuses, connectorRetryClasses } from "./generated-connector-contract.js";

const connectorActionStatusSet = new Set(connectorActionStatuses);
const connectorRetryClassSet = new Set(connectorRetryClasses);
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

export function assertConnectorActionResponse(value, expected) {
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
  return item;
}

export function isConnectorActionStatus(value) {
  return typeof value === "string" && connectorActionStatusSet.has(value);
}

export function isConnectorRetryPolicy(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  return (
    hasOnlyKeys(value, connectorRetryPolicyFields) &&
    typeof value.class === "string" &&
    connectorRetryClassSet.has(value.class) &&
    typeof value.guidance === "string" &&
    (value.precondition_fields === undefined ||
      (Array.isArray(value.precondition_fields) && value.precondition_fields.every((field) => typeof field === "string")))
  );
}

function hasOnlyKeys(value, allowed) {
  return Object.keys(value).every((field) => allowed.has(field));
}

function record(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid connector action response from gateway.");
  return value;
}

function positiveID(value) {
  return Number.isSafeInteger(value) && value > 0;
}

function nonEmptyString(value) {
  return typeof value === "string" && value.length > 0;
}

function optionalString(value) {
  return value === undefined || typeof value === "string";
}

function optionalRecord(value) {
  return value === undefined || (!!value && typeof value === "object" && !Array.isArray(value));
}

function optionalRetryAfterSeconds(value) {
  return value === undefined || (Number.isSafeInteger(value) && value >= 0 && value <= 3600);
}

function optionalBoolean(value) {
  return value === undefined || typeof value === "boolean";
}
