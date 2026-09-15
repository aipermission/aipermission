export type ConnectorActionResponse = Record<string, unknown> & {
  status: string;
  request_id?: number;
  display_text?: string;
  output?: Record<string, unknown>;
};

export type TokenActionPermission = Record<string, unknown> & {
  target_id: number;
  profile_id: number;
  action_name: string;
  execution_rule: string;
};

export type PendingApproval = Record<string, unknown> & {
  id: number;
  status: string;
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

export function connectorActionResponse(value: unknown): ConnectorActionResponse {
  const item = record(value, "connector action");
  if (typeof item.status !== "string" || !item.status || (item.output !== undefined && !recordOrNull(item.output))) {
    throw new Error("Invalid connector action response from gateway.");
  }
  if ((item.status === "running" || item.status === "approval_pending") && !positiveID(item.request_id)) {
    throw new Error("Pending connector action response is missing request_id.");
  }
  return item as ConnectorActionResponse;
}

function recordOrNull(value: unknown): boolean {
  return value === null || (typeof value === "object" && !Array.isArray(value));
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
      typeof item.execution_rule !== "string"
    ) {
      throw new Error("Invalid token permission response from gateway.");
    }
    return item as TokenActionPermission;
  });
}

export function pendingApprovals(value: unknown, context: string): PendingApproval[] {
  return array(value, context).map((entry) => {
    const item = record(entry, context);
    if (!positiveID(item.id) || typeof item.status !== "string") throw new Error(`Invalid ${context} response from gateway.`);
    return item as PendingApproval;
  });
}

export function consoleSessions(value: unknown): ConsoleSession[] {
  return array(value, "console sessions").map((entry) => {
    const item = record(entry, "console session");
    if (!positiveID(item.id)) throw new Error("Invalid console session response from gateway.");
    return item as ConsoleSession;
  });
}
