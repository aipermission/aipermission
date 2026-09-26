const successfulStatuses = new Set(["completed"]);
const nonTerminalStatuses = new Set(["approval_pending", "running"]);

type ActionItem = {
  status?: string;
  error?: string;
  display_text?: string;
  request_id?: number | string;
  output?: unknown;
};

export function connectorActionResultResponse(value: unknown): ActionItem {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid connector action result.");
  const data = value as Record<string, unknown>;
  if (typeof data.status !== "string") throw new Error("Invalid connector action result.");
  for (const key of ["error", "display_text"])
    if (data[key] !== undefined && typeof data[key] !== "string") throw new Error("Invalid connector action result.");
  if (
    data.request_id !== undefined &&
    typeof data.request_id !== "string" &&
    (typeof data.request_id !== "number" || !Number.isSafeInteger(data.request_id) || data.request_id < 1)
  )
    throw new Error("Invalid connector action result.");
  return data as ActionItem;
}

export function connectorActionError(item: ActionItem | null | undefined, fallback = "Connector action failed.") {
  if (item && (successfulStatuses.has(item.status || "") || nonTerminalStatuses.has(item.status || ""))) return "";
  return item?.error || item?.display_text || fallback;
}

export function connectorActionPending(item: ActionItem | null | undefined) {
  return Boolean(item && nonTerminalStatuses.has(item.status || ""));
}

export function connectorActionRequestID(item: ActionItem | null | undefined) {
  const requestID = Number(item?.request_id);
  if (!Number.isInteger(requestID) || requestID < 1) throw new Error("Pending connector action response is missing request_id.");
  return requestID;
}

export function connectorActionCode(item: ActionItem | null | undefined) {
  const output = item?.output;
  return String(output && typeof output === "object" && "code" in output ? output.code || "" : "");
}

export function requireCompletedConnectorAction<T extends ActionItem>(item: T, fallback = "Connector action failed."): T | null {
  const error = connectorActionError(item, fallback);
  if (error) {
    throw Object.assign(new Error(error), { actionItem: item });
  }
  return connectorActionPending(item) ? null : item;
}
