import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { expect, it, vi } from "vitest";
import { AppSidebar } from "./app-sidebar";

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
