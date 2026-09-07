import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router";
import { expect, it, vi } from "vitest";
import { ConsolePage } from "./console";

vi.mock("../components/console/console-workspace-panel", () => ({
  ConsoleWorkspacePanel: () => <div data-testid="console-workspace" />,
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

function gateway() {
  const resource = (data = []) => ({ state: "ready", data, error: null });
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
    newConsoleSession: vi.fn(),
    attachConsoleSession: vi.fn(),
    closeConsoleSession: vi.fn(),
    cancelConsoleCommand: vi.fn(),
    restartConsoleSession: vi.fn(),
    sendConsoleInput: vi.fn(),
    resizeConsoleSession: vi.fn(),
    runConnectorActionApproval: vi.fn(),
    declineConnectorActionApproval: vi.fn(),
  };
}
