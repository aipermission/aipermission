import { optionalConsoleText } from "./console-target-config";
import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";
import type { ConsoleSession } from "../../../lib/gateway-contracts/security-contracts";

export function runtimeConsoleSlotTarget(target: GatewayTarget, label: string) {
  return {
    ref: target.ref,
    target_id: target.target_id,
    profile_id: target.profile_id,
    runtime_id: target.runtime_id,
    config: {
      transport_target_ref: optionalConsoleText(target.config?.transport_target_ref, label, "transport_target_ref"),
    },
  };
}

export function liveConsoleSlotSession(session: unknown): Pick<ConsoleSession, "id" | "name"> | null {
  if (!session || typeof session !== "object" || Array.isArray(session) || !("id" in session)) return null;
  if (typeof session.id !== "number" || !Number.isSafeInteger(session.id) || session.id <= 0) return null;
  if (!("name" in session)) return { id: session.id };
  if (session.name !== undefined && typeof session.name !== "string") return null;
  return { id: session.id, name: session.name };
}
