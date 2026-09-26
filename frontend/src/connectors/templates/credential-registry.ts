import { supportedConnectorKinds } from "./catalog";
import { connectorKindFromPath } from "./template-registration";
import { isCredentialFamilyRegistration } from "./_shared/credential-family-registration";
import { captureCredentialFamily } from "../editor/capture-credential-family";
import type { RegisteredCredentialFamily } from "../editor/credential-family-types";

const modules = import.meta.glob("./*/index.ts", { eager: true, import: "credentialFamily" });
export const credentialFamilies = registerCredentialFamilies(modules, supportedConnectorKinds);

export function registerCredentialFamilies(modules: Record<string, unknown>, expectedKinds: readonly string[]) {
  const entries: [string, RegisteredCredentialFamily][] = Object.entries(modules).map(([path, registration]) => {
    const kind = connectorKindFromPath(path);
    if (!isCredentialFamilyRegistration(registration) || registration.kind !== kind) {
      throw new Error(`Connector ${kind} must export its native credentialFamily registration.`);
    }
    return [kind, registration.create(captureCredentialFamily)];
  });
  entries.sort(([left], [right]) => left.localeCompare(right));
  const actual = entries.map(([kind]) => kind);
  const expected = [...expectedKinds].sort();
  if (actual.length !== expected.length || actual.some((kind, index) => kind !== expected[index])) {
    throw new Error(`Credential family catalog/registry mismatch. catalog=${expected.join(",")} registry=${actual.join(",")}`);
  }
  const families: Readonly<Record<string, RegisteredCredentialFamily>> = Object.freeze(Object.fromEntries(entries));
  return families;
}
