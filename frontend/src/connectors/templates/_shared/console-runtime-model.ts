import type { GatewayTarget } from "../../../lib/gateway-contracts/core-resource-contracts";
import type { ConsoleRuntimeTarget } from "../../../components/use-gateway-resources";

type RuntimeProjection = Pick<ConsoleRuntimeTarget, "name" | "connector_kind" | "connector_ref" | "target_id" | "profile_id"> & {
  id?: string | number;
};

export function captureConsoleRuntimeProjection<Target, Runtime extends RuntimeProjection>(
  decodeTarget: (_target: GatewayTarget & { runtime_id: number }) => Target,
  project: (_options: { target: Target }) => Runtime,
): (_options: { target: GatewayTarget }) => ConsoleRuntimeTarget {
  return ({ target }) => {
    if (typeof target.runtime_id !== "number" || !Number.isSafeInteger(target.runtime_id) || target.runtime_id <= 0) {
      throw new Error("Console runtime projection requires a valid runtime identity.");
    }
    const runtime = project({ target: decodeTarget({ ...target, runtime_id: target.runtime_id }) });
    if (runtime.id !== target.runtime_id) throw new Error("Console runtime projection must preserve the runtime identity.");
    return { ...runtime, id: target.runtime_id, target };
  };
}
