import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { expect, it, vi } from "vitest";
import { AppMobileNavigation } from "./app-mobile-navigation";

it("opens mobile navigation and closes it after navigation", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <AppMobileNavigation
        sidebarProps={{
          pathname: "/",
          consoleAttentionCount: 0,
          activeTransferCount: 0,
          gatewayState: "running",
          mcpRuntime: { state: "ready", data: { enabled: false } },
          theme: "dark",
          onSetTheme: vi.fn(),
          onSetMCPRuntimeEnabled: vi.fn(),
          onOpenTransferCenter: vi.fn(),
          onSwitchDatabase: vi.fn(),
          onLockDatabase: vi.fn(),
        }}
      />
    </MemoryRouter>,
  );

  await user.click(screen.getByRole("button", { name: "Open navigation" }));
  expect(screen.getByRole("dialog", { name: "Navigation" })).toBeVisible();
  await user.click(screen.getByRole("link", { name: /Console/ }));
  expect(screen.queryByRole("dialog", { name: "Navigation" })).not.toBeInTheDocument();
});
