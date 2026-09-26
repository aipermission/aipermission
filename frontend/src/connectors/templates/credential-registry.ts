import { supportedConnectorKinds } from "./catalog";
import { registerNativeFamilies } from "./native-family-registry";
import { isCredentialFamilyRegistration } from "./_shared/credential-family-registration";
import { captureCredentialFamily } from "../editor/capture-credential-family";
import type { RegisteredCredentialFamily } from "../editor/credential-family-types";

const modules = import.meta.glob("./*/index.ts", { eager: true, import: "credentialFamily" });
export const credentialFamilies = registerCredentialFamilies(modules, supportedConnectorKinds);

export function registerCredentialFamilies(modules: Record<string, unknown>, expectedKinds: readonly string[]) {
  return registerNativeFamilies<RegisteredCredentialFamily>(modules, expectedKinds, "Credential family", (kind, registration) => {
    if (!isCredentialFamilyRegistration(registration) || registration.kind !== kind) {
      throw new Error(`Connector ${kind} must export its native credentialFamily registration.`);
    }
    return registration.create(captureCredentialFamily);
  });
}
