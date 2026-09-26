import type { CredentialFamilyDefinition, CredentialFamilyRegistration } from "../../editor/credential-family-types";

const registrations = new WeakSet<object>();

export function defineCredentialFamily<
  State extends object,
  Row extends { connector_kind: string },
  Target extends object,
  Operation extends string,
>(definition: CredentialFamilyDefinition<State, Row, Target, Operation>): CredentialFamilyRegistration {
  const captured = Object.freeze({ ...definition });
  const registration: CredentialFamilyRegistration = Object.freeze({
    kind: captured.kind,
    create: (capture) => capture(captured),
  });
  registrations.add(registration);
  return registration;
}

export function isCredentialFamilyRegistration(value: unknown): value is CredentialFamilyRegistration {
  return value !== null && typeof value === "object" && registrations.has(value);
}
