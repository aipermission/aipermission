import { allowedConnectorIcons } from "./catalog";
import { assertNetworkTransportMetadata } from "./_shared/network-transport-contract";
import { usesStandardTargetProfileLifecycle } from "./_shared/target-profile-lifecycle";

export const requiredModelFunctions = Object.freeze([
  "activeCredential",
  "canDelete",
  "canEdit",
  "credentialFormProps",
  "credentialHint",
  "credentialRows",
  "credentialStateFromRow",
  "deleteCredential",
  "deleteDialog",
  "deleteTarget",
  "emptyCredentialState",
  "emptyForm",
  "formFromTarget",
  "save",
  "saveCredential",
  "submitDisabled",
  "submitLabel",
  "syncForm",
  "targetDisplayName",
  "targetEndpoint",
  "targetProfileLabel",
  "targetSubtitle",
  "test",
  "recoverableRunningActions",
  "usesLiveConsole",
]);

// Runtime validation checks presence, not the native functions' call signatures.
export function assertConnectorTemplate(kind: string, template: unknown): void {
  if (!isRecord(template) || !isRecord(template.metadata)) {
    throw new Error(`Connector template ${kind} is missing metadata`);
  }
  const metadata = template.metadata;
  if (metadata.kind !== kind) {
    throw new Error(`Connector template ${kind} metadata kind must be ${kind}`);
  }
  for (const field of ["label", "summary", "version"]) {
    if (typeof metadata[field] !== "string" || !metadata[field].trim()) {
      throw new Error(`Connector template ${kind} metadata is missing ${field}`);
    }
  }
  if (typeof metadata.icon !== "string" || !allowedConnectorIcons.includes(metadata.icon)) {
    throw new Error(`Connector template ${kind} metadata icon must be one of: ${allowedConnectorIcons.join(", ")}`);
  }
  if (metadata.profile_lifecycle !== "standard" && metadata.profile_lifecycle !== "custom") {
    throw new Error(`Connector template ${kind} metadata profile_lifecycle must be standard or custom`);
  }
  assertNetworkTransportMetadata(kind, metadata.network_transport);
  for (const slot of ["Console", "CredentialForm", "Form", "RowActions"]) {
    if (typeof template[slot] !== "function") {
      throw new Error(`Connector template ${kind} is missing ${slot} slot`);
    }
  }
  if (!isRecord(template.model)) {
    throw new Error(`Connector template ${kind} is missing model exports`);
  }
  for (const fn of requiredModelFunctions) {
    if (typeof template.model[fn] !== "function") {
      throw new Error(`Connector template ${kind} model is missing ${fn}()`);
    }
  }
  if (metadata.profile_lifecycle === "standard" && !usesStandardTargetProfileLifecycle(template.model)) {
    throw new Error(`Connector template ${kind} standard profile lifecycle must use the shared executable contract`);
  }
}

export function connectorKindFromPath(path: string): string {
  const match = path.match(/^\.\/([^/]+)\/index\.ts$/);
  const kind = match?.[1];
  if (!kind) throw new Error(`Invalid connector template path: ${path}`);
  return kind;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
