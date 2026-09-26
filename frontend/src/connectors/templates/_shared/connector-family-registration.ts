import type { ConnectorEditorFormIdentity, ConnectorEditorOperation } from "../../editor/connector-editor-controller-types";
import type {
  ConnectorFamilyDefinition,
  ConnectorFamilyProfile,
  ConnectorFamilyRegistration,
  ConnectorFamilyTarget,
} from "../../editor/connector-family-types";

const registrations = new WeakSet<object>();

export function defineConnectorFamily<
  Form extends ConnectorEditorFormIdentity,
  Profile extends ConnectorFamilyProfile,
  Target extends ConnectorFamilyTarget<Profile>,
  Credential extends object,
  Active extends object,
  Operation extends ConnectorEditorOperation & { open: boolean },
>(definition: ConnectorFamilyDefinition<Form, Profile, Target, Credential, Active, Operation>): ConnectorFamilyRegistration {
  const captured = Object.freeze({ ...definition });
  const registration: ConnectorFamilyRegistration = Object.freeze({ kind: captured.kind, create: (capture) => capture(captured) });
  registrations.add(registration);
  return registration;
}

export function isConnectorFamilyRegistration(value: unknown): value is ConnectorFamilyRegistration {
  return value !== null && typeof value === "object" && registrations.has(value);
}
