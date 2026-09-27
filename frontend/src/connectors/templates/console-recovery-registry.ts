import { supportedConnectorKinds } from "./catalog";
import { registerNativeFamilies } from "./native-family-registry";
import { isConsoleSessionRecovery } from "./_shared/console-recovery";

const modules = import.meta.glob("./*/index.ts", { eager: true, import: "consoleRecovery" });
export const consoleRecoveries = registerConsoleRecoveries(modules, supportedConnectorKinds);

export function registerConsoleRecoveries(modules: Record<string, unknown>, expectedKinds: readonly string[]) {
  return registerNativeFamilies(modules, expectedKinds, "Console recovery", (kind, registration) => {
    if (registration === null) return null;
    if (!isConsoleSessionRecovery(registration) || registration.kind !== kind) {
      throw new Error(`Connector ${kind} must export its native consoleRecovery registration.`);
    }
    return registration;
  });
}

export function getConsoleSessionRecovery(kind: string | undefined) {
  return kind && Object.hasOwn(consoleRecoveries, kind) ? consoleRecoveries[kind] : null;
}
