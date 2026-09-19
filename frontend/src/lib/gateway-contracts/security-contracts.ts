import {
  connectorActionResponseRequiredFields,
  connectorActionStatuses,
  connectorRetryClasses,
  executionRules,
} from "./generated-connector-contract.js";
import { assertConnectorActionResponse, isConnectorActionStatus, isConnectorRetryPolicy } from "./connector-action-contract.js";

export { connectorActionResponseRequiredFields, connectorActionStatuses, connectorRetryClasses, executionRules };

export type ConnectorActionStatus = (typeof connectorActionStatuses)[number];
export type ConnectorRetryClass = (typeof connectorRetryClasses)[number];
export type ExecutionRule = (typeof executionRules)[number];

export type ConnectorRetryPolicy = {
  class: ConnectorRetryClass;
  guidance: string;
  precondition_fields?: string[];
};

export type ConnectorActionResponse = Record<string, unknown> & {
  status: ConnectorActionStatus;
  request_id: number;
  target_ref: string;
  connector_kind: string;
  action_name: string;
  approval_context_hash: string;
  retry_policy: ConnectorRetryPolicy;
  target_name?: string;
  profile_label?: string;
  input?: Record<string, unknown>;
  display_text?: string;
  output?: unknown;
  error?: string;
  retry_after_seconds?: number;
  assistant_hint?: string;
  output_withheld?: boolean;
  replayed?: boolean;
};

export type TokenActionPermission = Record<string, unknown> & {
  target_id: number;
  profile_id: number;
  action_name: string;
  execution_rule: ExecutionRule;
};

export type ConnectorApproval = Record<string, unknown> & {
  id: number;
  status: ConnectorActionStatus;
  target_id: number;
  target_name: string;
  target_ref: string;
  profile_id: number;
  profile_label: string;
  connector_kind: string;
  action_name: string;
  retry_policy: ConnectorRetryPolicy;
  created_at: string;
  approval_context_hash?: string;
};

export type VaultApproval = Record<string, unknown> & {
  id: number;
  status: string;
  token_id: number;
  token_name: string;
  project_id: number;
  project_name: string;
  project_slug: string;
  action_name: string;
  source: string;
  input: Record<string, unknown>;
  reason: string;
  approval_context_hash: string;
  idempotency_key: string;
  created_at: string;
  expires_at: string;
  updated_at: string;
};

export type ConsoleSession = Record<string, unknown> & {
  id: number;
};

function record(value: unknown, context: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`Invalid ${context} response from gateway.`);
  return value as Record<string, unknown>;
}

function positiveID(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) > 0;
}

function array(value: unknown, context: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`Invalid ${context} response from gateway.`);
  return value;
}

const executionRuleSet: ReadonlySet<string> = new Set(executionRules);

function executionRule(value: unknown): value is ExecutionRule {
  return typeof value === "string" && executionRuleSet.has(value);
}

export function connectorActionResponse(value: unknown, expected?: { targetRef: string; actionName: string }): ConnectorActionResponse {
  return assertConnectorActionResponse(value, expected) as ConnectorActionResponse;
}

function optionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function requiredRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function tokenActionPermissions(value: unknown): TokenActionPermission[] {
  const data = record(value, "token permissions");
  return array(data.items, "token permissions").map((entry) => {
    const item = record(entry, "token permission");
    if (
      !positiveID(item.target_id) ||
      !positiveID(item.profile_id) ||
      typeof item.action_name !== "string" ||
      !item.action_name ||
      !executionRule(item.execution_rule)
    ) {
      throw new Error("Invalid token permission response from gateway.");
    }
    return item as TokenActionPermission;
  });
}

export function connectorApprovals(value: unknown, context = "connector approvals"): ConnectorApproval[] {
  return array(value, context).map((entry) => {
    return connectorApproval(entry, undefined, context);
  });
}

export function connectorApproval(
  value: unknown,
  expected?: { id: number; targetRef: string; actionName: string; statuses?: readonly ConnectorActionStatus[] },
  context = "connector approval",
): ConnectorApproval {
  const item = record(value, context);
  if (
    !positiveID(item.id) ||
    !isConnectorActionStatus(item.status) ||
    !positiveID(item.target_id) ||
    !nonEmptyString(item.target_name) ||
    !nonEmptyString(item.target_ref) ||
    !positiveID(item.profile_id) ||
    !nonEmptyString(item.profile_label) ||
    !nonEmptyString(item.connector_kind) ||
    !nonEmptyString(item.action_name) ||
    !approvalContextHash(item.status, item.approval_context_hash, false) ||
    !isConnectorRetryPolicy(item.retry_policy) ||
    !nonEmptyString(item.created_at) ||
    (expected &&
      (item.id !== expected.id ||
        item.target_ref !== expected.targetRef ||
        item.action_name !== expected.actionName ||
        (expected.statuses && !expected.statuses.includes(item.status as ConnectorActionStatus))))
  ) {
    throw new Error(`Invalid ${context} response from gateway.`);
  }
  return item as ConnectorApproval;
}

const vaultApprovalStatuses = new Set(["approval_pending", "running", "completed", "failed", "declined", "stale", "canceled", "expired"]);

export function vaultApprovals(value: unknown, context = "Vault approvals"): VaultApproval[] {
  return array(value, context).map((entry) => {
    const item = record(entry, context);
    if (
      !positiveID(item.id) ||
      typeof item.status !== "string" ||
      !vaultApprovalStatuses.has(item.status) ||
      !positiveID(item.token_id) ||
      !nonEmptyString(item.token_name) ||
      !positiveID(item.project_id) ||
      !nonEmptyString(item.project_name) ||
      !nonEmptyString(item.project_slug) ||
      !nonEmptyString(item.action_name) ||
      !nonEmptyString(item.source) ||
      !requiredRecord(item.input) ||
      !optionalString(item.reason) ||
      !approvalContextHash(item.status, item.approval_context_hash, true) ||
      !nonEmptyString(item.idempotency_key) ||
      !nonEmptyString(item.created_at) ||
      !nonEmptyString(item.expires_at) ||
      !nonEmptyString(item.updated_at)
    ) {
      throw new Error(`Invalid ${context} response from gateway.`);
    }
    return item as VaultApproval;
  });
}

function approvalContextHash(status: unknown, value: unknown, alwaysPresent: boolean): boolean {
  if (status === "approval_pending") return nonEmptyString(value);
  return alwaysPresent ? typeof value === "string" : value === undefined || typeof value === "string";
}

export function consoleSessions(value: unknown): ConsoleSession[] {
  return array(value, "console sessions").map((entry) => {
    const item = record(entry, "console session");
    if (!positiveID(item.id)) throw new Error("Invalid console session response from gateway.");
    return item as ConsoleSession;
  });
}
