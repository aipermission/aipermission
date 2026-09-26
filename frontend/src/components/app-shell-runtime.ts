import { apiUrl, currentWorkspaceBinding } from "../lib/api.js";
import { isLiveConsoleSession } from "./console/helpers.ts";

type CredentialResource = {
  id?: string | number;
  name?: string;
  kind?: string;
  resource_kind?: string;
  resource_ref?: string;
  connector_kind?: string;
};
type RuntimeTarget = { connector_kind: string; runtime_id?: string | number };
type RuntimeProjection<Target, Result> = {
  usesLiveConsole?: (_options: { target: Target }) => boolean;
  liveConsoleRuntimeTarget?: (_options: { target: Target }) => Result;
};
type SessionSnapshot = { id: string | number; status?: string; transcript?: string; error?: string | null };

export function normalizeCredentialResources<T extends CredentialResource>(connectorKind: string, items: T[] | null | undefined) {
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

export function isActiveTransferBatch(batch: { status?: string } | null | undefined): boolean {
  return ["pending_approval", "pending", "running", "paused"].includes(batch?.status || "");
}

export function consoleSessionAttachUrl(sessionID: string | number): string {
  const url = new URL(apiUrl, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = `/api/console/sessions/${sessionID}/attach`;
  url.searchParams.set("workspace", currentWorkspaceBinding());
  return url.toString();
}

export function limitTranscript(value: string): string {
  const maxLength = 200000;
  return value.length <= maxLength ? value : value.slice(value.length - maxLength);
}

export function parseConsoleSocketMessage(value: unknown): { type: string; [field: string]: unknown } | null {
  if (typeof value !== "string") return null;
  try {
    const message: unknown = JSON.parse(value);
    if (!message || typeof message !== "object" || Array.isArray(message) || !("type" in message) || typeof message.type !== "string")
      return null;
    return { ...Object.fromEntries(Object.entries(message)), type: message.type };
  } catch {
    return null;
  }
}

export function createPollGenerationGuard() {
  let current = 0;
  return {
    begin() {
      current += 1;
      return current;
    },
    isCurrent(generation?: number) {
      return generation === undefined || generation === current;
    },
    invalidate() {
      current += 1;
    },
  };
}

export function liveConsoleRuntimeTargets<Target extends RuntimeTarget, Result>(
  targets: Target[] | null | undefined,
  getModel: (_kind: string) => RuntimeProjection<Target, Result> | null | undefined,
): Result[] {
  const runtimes: Result[] = [];
  for (const target of targets || []) {
    const model = getModel(target.connector_kind);
    if (model?.usesLiveConsole?.({ target }) && target.runtime_id && model.liveConsoleRuntimeTarget) {
      runtimes.push(model.liveConsoleRuntimeTarget({ target }));
    }
  }
  return runtimes;
}

export function mergeConsoleSessionData<T extends SessionSnapshot>(next: T[], current: T[]): T[] {
  return next.map((session) => {
    const local = current.find((item) => Number(item.id) === Number(session.id));
    if (!local) return session;
    if (isLiveConsoleSession(local) && isLiveConsoleSession(session)) {
      return { ...session, transcript: local.transcript, status: local.status, error: local.error };
    }
    return session;
  });
}
