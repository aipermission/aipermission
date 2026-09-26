import { render } from "@testing-library/react";
import { vi } from "vitest";
import type {
  CredentialFamilyCommands,
  CredentialFamilyProps,
  RegisteredCredentialFamily,
} from "../connectors/editor/credential-family-types";

export function renderCredentialFamily(family: RegisteredCredentialFamily, overrides: Partial<CredentialFamilyProps> = {}) {
  let commands: CredentialFamilyCommands | null = null;
  const refresh = vi.fn(async () => {});
  const onStateChange = vi.fn();
  const onOpen = vi.fn();
  const register = vi.fn((_kind: string, next: CredentialFamilyCommands | null) => {
    commands = next;
  });
  const props: CredentialFamilyProps = {
    targets: [],
    credentials: [],
    busy: false,
    refresh,
    onOpen,
    onStateChange,
    register,
    ...overrides,
  };
  const result = render(
    <table>
      <tbody>
        <family.Rows {...props} />
      </tbody>
    </table>,
  );
  return {
    ...result,
    props,
    refresh,
    onStateChange,
    onOpen,
    register,
    openCreate: () => {
      if (!commands) throw new Error("Native credential commands were not registered");
      commands.openCreate();
    },
    close: () => commands?.close(),
  };
}
