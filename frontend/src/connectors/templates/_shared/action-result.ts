const successfulStatuses = new Set(["completed"]);
const nonTerminalStatuses = new Set(["approval_pending", "running"]);

type ActionItem = {
  status?: string;
  error?: string;
  display_text?: string;
  request_id?: number | string;
  output?: Record<string, unknown>;
};

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
  return String(item?.output?.code || "");
}

export function requireCompletedConnectorAction<T extends ActionItem>(item: T, fallback = "Connector action failed."): T | null {
  const error = connectorActionError(item, fallback);
  if (error) {
    throw Object.assign(new Error(error), { actionItem: item });
  }
  return connectorActionPending(item) ? null : item;
}
