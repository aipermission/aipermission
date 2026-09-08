import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router";
import { expect, it, vi } from "vitest";
import { ConsolePage } from "./console";

let workspaceProps;
vi.mock("../components/console/console-workspace-panel", () => ({
  ConsoleWorkspacePanel: (props) => {
    workspaceProps = props;
    return <div data-testid="console-workspace" />;
  },
}));

it("renders the console route before a target is available", () => {
  render(
    <MemoryRouter initialEntries={["/console"]}>
      <Routes>
        <Route element={<Outlet context={gateway()} />}>
          <Route path="/console" element={<ConsolePage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

  expect(screen.getByTestId("console-workspace")).toBeVisible();
  expect(screen.getByPlaceholderText("Search connectors")).toBeVisible();
});

it("wires live-console actions to the selected runtime session", async () => {
  const target = {
    ref: "ssh:2:20",
    connector_kind: "ssh",
    target_id: 2,
    profile_id: 20,
    profile_label: "root",
    runtime_id: 2,
    name: "ops",
  };
  const context = gateway({
    targets: resource([target]),
    liveConsoleTargets: resource([{ id: 2, name: "ops" }]),
    consoleSessions: resource([{ id: 9, runtime_id: 2, status: "connected", transcript: "" }]),
  });
  render(
    <MemoryRouter initialEntries={["/console?target=ssh%3A2%3A20"]}>
      <Routes>
        <Route element={<Outlet context={context} />}>
          <Route path="/console" element={<ConsolePage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

  await act(async () => {
    workspaceProps.actions.endLiveSession();
    workspaceProps.actions.interruptSession();
    workspaceProps.actions.resizeSession(120, 40);
    workspaceProps.actions.sendInput("uptime\n");
    workspaceProps.actions.startLiveSession();
    await workspaceProps.actions.startLiveSessionWithOptions({ vault_item_ids: [4] });
  });

  expect(context.closeConsoleSession).toHaveBeenCalledWith(9);
  expect(context.cancelConsoleCommand).toHaveBeenCalledWith(9);
  expect(context.resizeConsoleSession).toHaveBeenCalledWith(9, 120, 40);
  expect(context.sendConsoleInput).toHaveBeenCalledWith(9, "uptime\n");
  expect(context.newConsoleSession).toHaveBeenCalled();
});

function resource(data = []) {
  return { state: "ready", data, error: null };
}

function gateway(overrides = {}) {
  return {
    liveConsoleTargets: resource(),
    targets: resource(),
    tokens: resource(),
    connectorActionApprovals: resource(),
    messages: resource(),
    consoleSessions: resource(),
    mcpRuntime: resource({ enabled: false }),
    theme: "dark",
    loadConsoleSessions: vi.fn(),
    loadTokens: vi.fn(),
    loadTargets: vi.fn(),
    loadConnectorActionApprovals: vi.fn(),
    loadMessages: vi.fn(),
    markRuntimeMessagesRead: vi.fn(),
    newConsoleSession: vi.fn(async () => ({ id: 10, runtime_id: 2, status: "connecting" })),
    attachConsoleSession: vi.fn(),
    closeConsoleSession: vi.fn(async () => {}),
    cancelConsoleCommand: vi.fn(),
    restartConsoleRuntime: vi.fn(),
    sendConsoleInput: vi.fn(),
    resizeConsoleSession: vi.fn(),
    runConnectorActionApproval: vi.fn(),
    declineConnectorActionApproval: vi.fn(),
    ...overrides,
  };
}
