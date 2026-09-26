import { supportedConnectorKinds } from "./catalog";
import { registerNativeFamilies } from "./native-family-registry";
import { isConnectorFamilyRegistration } from "./_shared/connector-family-registration";
import { captureConnectorFamily } from "../editor/capture-connector-family";
import type { RegisteredConnectorFamily } from "../editor/connector-family-types";

const modules = import.meta.glob("./*/index.ts", { eager: true, import: "connectorFamily" });
export const connectorFamilies = registerConnectorFamilies(modules, supportedConnectorKinds);

export function registerConnectorFamilies(modules: Record<string, unknown>, expectedKinds: readonly string[]) {
  return registerNativeFamilies<RegisteredConnectorFamily>(modules, expectedKinds, "Connector family", (kind, registration) => {
    if (!isConnectorFamilyRegistration(registration) || registration.kind !== kind) {
      throw new Error(`Connector ${kind} must export its native connectorFamily registration.`);
    }
    return registration.create(captureConnectorFamily);
  });
}
