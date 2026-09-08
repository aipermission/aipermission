import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TokenPermissionPanel } from "./token-permission-panel";

vi.mock("./connector-token-permission-panel", () => ({
  ConnectorTokenPermissionPanel: (props) => <div data-testid="permission-panel">{JSON.stringify(props)}</div>,
}));

it("forwards the generic connector permission contract", () => {
  const props = {
    tokens: [{ id: 1 }],
    selectedTarget: { id: 2 },
    targets: [{ id: 2 }],
    unreadMessages: 3,
    compact: true,
    connectorPermissionState: { selectedProfileID: 4 },
    loadAllConnectorPermissions: vi.fn(),
    loadConnectorActions: vi.fn(),
    replaceTokenConnectorPermissions: vi.fn(),
    onToggleCompact: vi.fn(),
    onRefresh: vi.fn(),
    onOpenMessages: vi.fn(),
  };
  render(<TokenPermissionPanel {...props} />);
  const forwarded = screen.getByTestId("permission-panel").textContent;
  expect(forwarded).toContain('"unreadMessages":3');
  expect(forwarded).toContain('"compact":true');
  expect(forwarded).toContain('"selectedProfileID":4');
});
