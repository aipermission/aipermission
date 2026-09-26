import { act, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { ConsolePage } from "./console";
import type { ComponentProps } from "react";
import type { GatewayContext } from "../lib/gateway-context";
import type { ConsoleWorkspacePanel } from "../components/console/console-workspace-panel";
import { gatewayTargetFixture } from "../test/connector-inventory-fixtures";

let workspaceProps: ComponentProps<typeof ConsoleWorkspacePanel> | undefined;
const media = vi.hoisted(() => ({ wide: false }));
vi.mock("../lib/use-media-query", () => ({ useMediaQuery: () => media.wide }));
vi.mock("../components/console/console-workspace-panel", () => ({
  ConsoleWorkspacePanel: (props: ComponentProps<typeof ConsoleWorkspacePanel>) => {
    workspaceProps = props;
    return <div data-testid="console-workspace" />;
  },
}));

beforeEach(() => {
  workspaceProps = undefined;
  media.wide = false;
});

it("does not dispatch live-session controls before a runtime session exists", async () => {
  media.wide = true;
  const context = gateway();
  render(
    <MemoryRouter initialEntries={["/console"]}>
      <Routes>
        <Route element={<Outlet context={context} />}>
          <Route path="/console" element={<ConsolePage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
  await act(async () => {
    if (!workspaceProps) throw new Error("Console workspace did not render");
    workspaceProps.actions.endLiveSession();
    workspaceProps.actions.interruptSession();
    workspaceProps.actions.resizeSession(120, 40);
    workspaceProps.actions.sendInput("uptime\n");
    workspaceProps.actions.startLiveSession();
    await workspaceProps.actions.startLiveSessionWithOptions();
  });
  expect(context.closeConsoleSession).not.toHaveBeenCalled();
  expect(context.cancelConsoleCommand).not.toHaveBeenCalled();
  expect(context.resizeConsoleSession).not.toHaveBeenCalled();
  expect(context.sendConsoleInput).not.toHaveBeenCalled();
  expect(context.newConsoleSession).not.toHaveBeenCalled();
  fireEvent.click(screen.getByTitle("Collapse tokens"));
  expect(screen.getByTitle("Expand tokens")).toBeVisible();
  fireEvent.click(screen.getByTitle("Expand tokens"));
  expect(screen.getByTitle("Collapse tokens")).toBeVisible();
  fireEvent.click(screen.getByTitle("Refresh connector permissions"));
  await act(async () => {});
  expect(context.loadTokens).toHaveBeenCalledOnce();
  expect(context.loadTargets).toHaveBeenCalledOnce();
});

it("renders the console route and exposes connector navigation on narrow screens", () => {
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
  fireEvent.click(screen.getByRole("button", { name: "Connectors" }));
  expect(screen.getByPlaceholderText("Search connectors")).toBeVisible();
});

it("wires live-console actions to the selected runtime session", async () => {
  const target = gatewayTargetFixture({
    ref: "ssh:2:20",
    connector_kind: "ssh",
    target_id: 2,
    profile_id: 20,
    profile_label: "root",
    runtime_id: 2,
    name: "ops",
  });
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
    if (!workspaceProps) throw new Error("Console workspace did not render");
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

function resource<Data>(data: Data) {
  return { state: "ready" as const, data, error: null };
}

function gateway(overrides: Partial<GatewayContext> = {}) {
  return {
    liveConsoleTargets: resource([]),
    targets: resource([]),
    tokens: resource([]),
    connectorActionApprovals: resource([]),
    messages: resource([]),
    consoleSessions: resource([]),
    mcpRuntime: resource({ enabled: false, start_enabled: false }),
    theme: "dark",
    loadConsoleSessions: vi.fn().mockResolvedValue([]),
    loadTokens: vi.fn().mockResolvedValue([]),
    loadTargets: vi.fn().mockResolvedValue([]),
    loadConnectorActionApprovals: vi.fn().mockResolvedValue([]),
    loadMessages: vi.fn().mockResolvedValue([]),
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
  } satisfies Partial<GatewayContext>;
}
