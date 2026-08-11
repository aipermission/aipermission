import { apiUrl } from "../lib/api.js";
import { isLiveConsoleSession } from "./console/helpers.js";

export function normalizeCredentialResources(connectorKind, items) {
  return (items || []).map((item) => {
    const resourceKind = item.resource_kind || item.kind || "credential";
    return {
      ...item,
      connector_kind: item.connector_kind || connectorKind,
      resource_kind: resourceKind,
      resource_ref: item.resource_ref || `${connectorKind}:${resourceKind}:${item.id || item.name || "unknown"}`,
    };
  });
}

export function isActiveTransferBatch(batch) {
  return ["pending_approval", "pending", "running", "paused"].includes(batch?.status);
}

export function consoleSessionAttachUrl(sessionID) {
  const url = new URL(apiUrl, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = `/api/console/sessions/${sessionID}/attach`;
  return url.toString();
}

export function limitTranscript(value) {
  const maxLength = 200000;
  return value.length <= maxLength ? value : value.slice(value.length - maxLength);
}

export function liveConsoleRuntimeTargets(targets, getModel) {
  return (targets || [])
    .filter((target) => {
      const model = getModel(target.connector_kind);
      return Boolean(model?.usesLiveConsole?.({ target }) && target.runtime_id && model?.liveConsoleRuntimeTarget);
    })
    .map((target) => getModel(target.connector_kind).liveConsoleRuntimeTarget({ target }));
}

export function mergeConsoleSessionData(next, current) {
  return next.map((session) => {
    const local = current.find((item) => Number(item.id) === Number(session.id));
    if (!local) return session;
    if (isLiveConsoleSession(local) && isLiveConsoleSession(session)) {
      return { ...session, transcript: local.transcript, status: local.status, error: local.error };
    }
    return session;
  });
}
