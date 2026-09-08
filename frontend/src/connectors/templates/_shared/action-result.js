const successfulStatuses = new Set(["completed"]);
const nonTerminalStatuses = new Set(["approval_pending", "running"]);

export function connectorActionError(item, fallback = "Connector action failed.") {
  if (item && (successfulStatuses.has(item.status) || nonTerminalStatuses.has(item.status))) return "";
  return item?.error || item?.display_text || fallback;
}

export function connectorActionPending(item) {
  return Boolean(item && nonTerminalStatuses.has(item.status));
}

export function connectorActionRequestID(item) {
  const requestID = Number(item?.request_id);
  if (!Number.isInteger(requestID) || requestID < 1) throw new Error("Pending connector action response is missing request_id.");
  return requestID;
}

export function connectorActionCode(item) {
  return String(item?.output?.code || "");
}

export function requireCompletedConnectorAction(item, fallback = "Connector action failed.") {
  const error = connectorActionError(item, fallback);
  if (error) {
    const failure = new Error(error);
    failure.actionItem = item;
    throw failure;
  }
  return connectorActionPending(item) ? null : item;
}
