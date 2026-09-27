import { supportedConnectorKinds } from "./catalog";
import { registerNativeFamilies } from "./native-family-registry";
import { isConsolePresentationModel } from "./_shared/console-presentation";
import type { ConsolePresentationModel } from "./_shared/console-presentation-types";

const modules = import.meta.glob("./*/index.ts", { eager: true, import: "consoleModel" });
export const consoleModels = registerConsoleModels(modules, supportedConnectorKinds);

export function registerConsoleModels(modules: Record<string, unknown>, expectedKinds: readonly string[]) {
  return registerNativeFamilies<Readonly<ConsolePresentationModel>>(modules, expectedKinds, "Console model", (kind, registration) => {
    if (!isConsolePresentationModel(registration) || registration.kind !== kind) {
      throw new Error(`Connector ${kind} must export its native consoleModel registration.`);
    }
    return registration;
  });
}

export function getConsolePresentationModel(kind: string | undefined) {
  return kind && Object.hasOwn(consoleModels, kind) ? consoleModels[kind] : null;
}
