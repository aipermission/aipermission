import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TokenPermissionPanel } from "./token-permission-panel";
import type { ComponentProps } from "react";

vi.mock("./connector-token-permission-panel", () => ({
  ConnectorTokenPermissionPanel: (props: ComponentProps<typeof TokenPermissionPanel>) => (
    <div data-testid="permission-panel">{JSON.stringify(props)}</div>
  ),
}));

it("forwards the generic connector permission contract", () => {
  const props: ComponentProps<typeof TokenPermissionPanel> = {
    tokens: { state: "ready", data: [{ id: 1, name: "Agent" }] },
    selectedTarget: { connector_kind: "fixture", target_id: 2, profile_id: 4 },
    targets: { data: [{ connector_kind: "fixture", target_id: 2, profile_id: 4 }] },
    unreadMessages: [{ token_id: 1, runtime_id: 3 }],
    compact: true,
    connectorPermissionState: { state: "ready", data: {}, revisionsByToken: {}, actionsByTargetRef: {}, error: null },
    loadAllConnectorPermissions: vi.fn(),
    loadConnectorActions: vi.fn(),
    replaceTokenConnectorPermissions: vi.fn(),
    onToggleCompact: vi.fn(),
    onRefresh: vi.fn(),
    onOpenMessages: vi.fn(),
  };
  render(<TokenPermissionPanel {...props} />);
  const forwarded = screen.getByTestId("permission-panel").textContent;
  expect(forwarded).toContain('"runtime_id":3');
  expect(forwarded).toContain('"compact":true');
  expect(forwarded).toContain('"profile_id":4');
});

it("defaults to expanded permissions when callers omit compact mode", () => {
  render(
    <TokenPermissionPanel
      tokens={{ state: "ready", data: [] }}
      selectedTarget={null}
      targets={{ data: [] }}
      connectorPermissionState={{ state: "ready", data: {}, revisionsByToken: {}, actionsByTargetRef: {}, error: null }}
      loadAllConnectorPermissions={vi.fn()}
      loadConnectorActions={vi.fn()}
      replaceTokenConnectorPermissions={vi.fn()}
      onRefresh={vi.fn()}
    />,
  );
  expect(screen.getByTestId("permission-panel")).toHaveTextContent('"compact":false');
});
