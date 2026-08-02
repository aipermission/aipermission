const successfulStatuses = new Set(["completed"]);
const nonTerminalStatuses = new Set(["approval_pending", "running"]);

export function connectorActionError(item, fallback = "Connector action failed.") {
  if (item && (successfulStatuses.has(item.status) || nonTerminalStatuses.has(item.status))) return "";
  return item?.error || item?.display_text || fallback;
}

export function connectorActionPending(item) {
  return Boolean(item && nonTerminalStatuses.has(item.status));
}

export function connectorActionCode(item) {
  return String(item?.output?.code || "");
}
