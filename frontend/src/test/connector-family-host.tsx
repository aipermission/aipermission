import { render } from "@testing-library/react";
import { vi } from "vitest";
import type { ConnectorFamilyCommands, ConnectorFamilyProps, RegisteredConnectorFamily } from "../connectors/editor/connector-family-types";

export function renderConnectorFamily(family: RegisteredConnectorFamily, overrides: Partial<Omit<ConnectorFamilyProps, "children">> = {}) {
  let registered: ConnectorFamilyCommands | null = null;
  const props: Omit<ConnectorFamilyProps, "children"> = {
    targets: [],
    credentials: [],
    projects: [{ id: 7, name: "My Project" }],
    firstCredentialID: "",
    defaultProjectID: 7,
    connectorOptions: [{ kind: family.kind, label: family.kind }],
    busy: false,
    onOpen: vi.fn(),
    onSelectKind: vi.fn(),
    onStateChange: vi.fn(),
    onTestsChange: vi.fn(),
    refresh: vi.fn(async () => {}),
    ...overrides,
    register: (kind, commands) => {
      registered = commands;
      overrides.register?.(kind, commands);
    },
  };
  const Provider = family.Provider;
  const view = render(
    <Provider {...props}>
      <div>Inventory</div>
    </Provider>,
  );
  return {
    ...view,
    props,
    commands: () => {
      if (!registered) throw new Error("Connector family commands are not registered.");
      return registered;
    },
  };
}
