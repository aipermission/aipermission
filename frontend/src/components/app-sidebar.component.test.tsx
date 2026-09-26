import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { AppSidebar } from "./app-sidebar";
import type { AppSidebarProps } from "./app-sidebar";
import { checkForUpdates } from "../lib/update-check";

vi.mock("../lib/update-check", () => ({ checkForUpdates: vi.fn() }));
beforeEach(() => {
  vi.mocked(checkForUpdates).mockReset();
});

function renderSidebar(overrides: Partial<AppSidebarProps> = {}) {
  const props: AppSidebarProps = {
    pathname: "/",
    consoleAttentionCount: 0,
    activeTransferCount: 0,
    gatewayState: "running",
    mcpRuntime: { state: "ready", data: { enabled: true } },
    theme: "dark",
    onSetTheme: vi.fn(),
    onSetMCPRuntimeEnabled: vi.fn(),
    onOpenTransferCenter: vi.fn(),
    onSwitchDatabase: vi.fn(),
    onLockDatabase: vi.fn(),
    ...overrides,
  };
  render(
    <MemoryRouter>
      <AppSidebar {...props} />
    </MemoryRouter>,
  );
  return props;
}

it("renders the complete embedded navigation and reports route selection", async () => {
  const user = userEvent.setup();
  const onNavigate = vi.fn();
  render(
    <MemoryRouter>
      <AppSidebar
        embedded
        pathname="/"
        consoleAttentionCount={2}
        activeTransferCount={0}
        gatewayState="running"
        mcpRuntime={{ state: "ready", data: { enabled: false } }}
        theme="dark"
        onSetTheme={vi.fn()}
        onSetMCPRuntimeEnabled={vi.fn()}
        onOpenTransferCenter={vi.fn()}
        onSwitchDatabase={vi.fn()}
        onLockDatabase={vi.fn()}
        onNavigate={onNavigate}
      />
    </MemoryRouter>,
  );

  expect(screen.getByRole("link", { name: /Dashboard/ })).toBeVisible();
  await user.click(screen.getByRole("link", { name: /Console/ }));
  expect(onNavigate).toHaveBeenCalledOnce();
});

it("dispatches workspace controls and safely reports non-Error runtime failures", async () => {
  const user = userEvent.setup();
  const props = renderSidebar({
    onSetMCPRuntimeEnabled: vi.fn(async () => {
      throw null;
    }),
  });
  await user.click(screen.getByRole("button", { name: "Stop MCP" }));
  expect(props.onSetMCPRuntimeEnabled).toHaveBeenCalledWith(false);
  expect(await screen.findByText("MCP runtime update failed.")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Light" }));
  await user.click(screen.getByRole("button", { name: /Transfers/ }));
  await user.click(screen.getByRole("button", { name: "Switch" }));
  await user.click(screen.getByRole("button", { name: "Lock" }));
  expect(props.onSetTheme).toHaveBeenCalledWith("light");
  expect(props.onOpenTransferCenter).toHaveBeenCalledOnce();
  expect(props.onSwitchDatabase).toHaveBeenCalledOnce();
  expect(props.onLockDatabase).toHaveBeenCalledOnce();
});

it("checks updates only on demand and shows a validated release result", async () => {
  const user = userEvent.setup();
  vi.mocked(checkForUpdates).mockResolvedValue({
    latestVersion: "9.0.0",
    localVersion: "1.0.0",
    releaseUrl: "https://github.com/aipermission/aipermission/releases",
    updateAvailable: true,
  });
  renderSidebar();
  expect(checkForUpdates).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: /Changelog/ }));
  await user.click(screen.getByRole("button", { name: "Check for updates" }));
  expect(await screen.findByText(/Update available: 9.0.0/)).toBeVisible();
  expect(screen.getByRole("link", { name: "View releases" })).toHaveAttribute(
    "href",
    "https://github.com/aipermission/aipermission/releases",
  );
});

it("leaves the checking state after a malformed update failure", async () => {
  const user = userEvent.setup();
  vi.mocked(checkForUpdates).mockRejectedValue(undefined);
  renderSidebar();
  await user.click(screen.getByRole("button", { name: /Changelog/ }));
  await user.click(screen.getByRole("button", { name: "Check for updates" }));
  expect(await screen.findByText("Update check failed.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Check for updates" })).toBeEnabled();
});
