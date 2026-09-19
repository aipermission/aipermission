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
  approval_context_hash?: string;
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
  token_id?: number;
  token_name?: string;
  target_id: number;
  target_name: string;
  target_ref: string;
  profile_id: number;
  profile_label: string;
  connector_kind: string;
  action_name: string;
  title?: string;
  summary?: string;
  preview?: Record<string, unknown>;
  input?: Record<string, unknown>;
  reason?: string;
  display_text?: string;
  error?: string;
  retry_policy: ConnectorRetryPolicy;
  created_at: string;
  completed_at?: string;
  retry_after_seconds?: number;
  assistant_hint?: string;
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
  approval_context?: VaultApprovalContext;
  approval_context_hash: string;
  idempotency_key: string;
  created_at: string;
  expires_at: string;
  updated_at: string;
};

export type VaultSessionItem = {
  item_id: number;
  name: string;
  source_project_id: number;
  value_version: number;
  metadata_revision: number;
  replace_existing: boolean;
  binding_id?: number;
  binding_revision?: number;
};

export type VaultApprovalContext = Record<string, unknown> & {
  items?: VaultSessionItem[];
  target_id?: number;
  profile_id?: number;
  connector_kind?: string;
  expected_session_id?: number;
};

type ExpectedVaultApproval = {
  id: number;
  tokenID?: number;
  projectID?: number;
  actionName?: string;
  statuses?: readonly string[];
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

function optionalNonEmptyString(value: unknown): boolean {
  return value === undefined || nonEmptyString(value);
}

function optionalPositiveID(value: unknown): boolean {
  return value === undefined || positiveID(value);
}

function optionalRecord(value: unknown): boolean {
  return value === undefined || requiredRecord(value);
}

function optionalRetryAfterSeconds(value: unknown): boolean {
  return value === undefined || (Number.isSafeInteger(value) && (value as number) >= 0 && (value as number) <= 3600);
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

type ExpectedConnectorApproval = {
  id: number;
  targetRef: string;
  actionName: string;
  statuses?: readonly ConnectorActionStatus[];
};

function validConnectorApprovalFields(item: Record<string, unknown>): boolean {
  return (
    positiveID(item.id) &&
    isConnectorActionStatus(item.status) &&
    optionalPositiveID(item.token_id) &&
    optionalNonEmptyString(item.token_name) &&
    positiveID(item.target_id) &&
    nonEmptyString(item.target_name) &&
    nonEmptyString(item.target_ref) &&
    positiveID(item.profile_id) &&
    nonEmptyString(item.profile_label) &&
    nonEmptyString(item.connector_kind) &&
    nonEmptyString(item.action_name) &&
    optionalString(item.title) &&
    optionalString(item.summary) &&
    optionalRecord(item.preview) &&
    optionalRecord(item.input) &&
    optionalString(item.reason) &&
    optionalString(item.display_text) &&
    optionalString(item.error) &&
    approvalContextHash(item.status, item.approval_context_hash, false) &&
    isConnectorRetryPolicy(item.retry_policy) &&
    nonEmptyString(item.created_at) &&
    optionalNonEmptyString(item.completed_at) &&
    optionalRetryAfterSeconds(item.retry_after_seconds) &&
    optionalString(item.assistant_hint)
  );
}

function matchesExpectedConnectorApproval(item: Record<string, unknown>, expected?: ExpectedConnectorApproval): boolean {
  return (
    !expected ||
    (item.id === expected.id &&
      item.target_ref === expected.targetRef &&
      item.action_name === expected.actionName &&
      (!expected.statuses || expected.statuses.includes(item.status as ConnectorActionStatus)))
  );
}

export function connectorApproval(value: unknown, expected?: ExpectedConnectorApproval, context = "connector approval"): ConnectorApproval {
  const item = record(value, context);
  if (!validConnectorApprovalFields(item) || !matchesExpectedConnectorApproval(item, expected)) {
    throw new Error(`Invalid ${context} response from gateway.`);
  }
  return item as ConnectorApproval;
}

const vaultApprovalStatuses = new Set(["approval_pending", "running", "completed", "failed", "declined", "stale", "canceled", "expired"]);

export function vaultApprovals(value: unknown, context = "Vault approvals"): VaultApproval[] {
  return array(value, context).map((entry) => vaultApproval(entry, undefined, context));
}

export function vaultApproval(value: unknown, expected?: ExpectedVaultApproval, context = "Vault approval"): VaultApproval {
  const item = record(value, context);
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
    !validVaultApprovalContext(item) ||
    !approvalContextHash(item.status, item.approval_context_hash, true) ||
    !nonEmptyString(item.idempotency_key) ||
    !nonEmptyString(item.created_at) ||
    !nonEmptyString(item.expires_at) ||
    !nonEmptyString(item.updated_at) ||
    !matchesExpectedVaultApproval(item, expected)
  ) {
    throw new Error(`Invalid ${context} response from gateway.`);
  }
  return item as VaultApproval;
}

function matchesExpectedVaultApproval(item: Record<string, unknown>, expected?: ExpectedVaultApproval): boolean {
  if (!expected) return true;
  return (
    item.id === expected.id &&
    (expected.tokenID === undefined || item.token_id === expected.tokenID) &&
    (expected.projectID === undefined || item.project_id === expected.projectID) &&
    (expected.actionName === undefined || item.action_name === expected.actionName) &&
    (!expected.statuses || expected.statuses.includes(item.status as string))
  );
}

function validVaultApprovalContext(item: Record<string, unknown>): boolean {
  const value = item.approval_context;
  if (item.status === "approval_pending") return validPendingVaultApprovalContext(item, value);
  if (value === undefined) return true;
  if (!requiredRecord(value)) return false;
  return validVaultSessionContextFields(value);
}

function validPendingVaultApprovalContext(item: Record<string, unknown>, value: unknown): boolean {
  if (!requiredRecord(value) || !validVaultSessionContextFields(value)) return false;
  if (
    !nonEmptyString(value.schema) ||
    value.action_name !== item.action_name ||
    value.token_id !== item.token_id ||
    value.project_id !== item.project_id ||
    !nonEmptyString(value.workspace_id) ||
    !nonEmptyString(value.runtime_instance_id) ||
    !nonEmptyString(value.capability_name) ||
    !nonEmptyString(value.execution_rule) ||
    !nonEmptyString(value.input_hash) ||
    !nonEmptyString(value.project_scope_hash)
  ) {
    return false;
  }
  if (item.action_name !== "restart_session_with_environment") return item.action_name === "generate_item";
  return (
    positiveID(value.target_id) &&
    positiveID(value.profile_id) &&
    nonEmptyString(value.connector_kind) &&
    Array.isArray(value.items) &&
    value.items.length > 0
  );
}

function validVaultSessionContextFields(value: Record<string, unknown>): boolean {
  for (const field of ["target_id", "profile_id", "expected_session_id"]) {
    if (value[field] !== undefined && !positiveID(value[field])) return false;
  }
  if (value.connector_kind !== undefined && !nonEmptyString(value.connector_kind)) return false;
  if (value.items === undefined) return true;
  if (!Array.isArray(value.items)) return false;
  return value.items.every((entry) => {
    if (!requiredRecord(entry)) return false;
    return (
      positiveID(entry.item_id) &&
      nonEmptyString(entry.name) &&
      positiveID(entry.source_project_id) &&
      positiveID(entry.value_version) &&
      positiveID(entry.metadata_revision) &&
      typeof entry.replace_existing === "boolean" &&
      (entry.binding_id === undefined || positiveID(entry.binding_id)) &&
      (entry.binding_revision === undefined || positiveID(entry.binding_revision))
    );
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
