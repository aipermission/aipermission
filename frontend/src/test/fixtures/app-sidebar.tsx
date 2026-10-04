import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { vi } from "vitest";
import { AppSidebar } from "../../components/app-sidebar";
import type { AppSidebarProps } from "../../components/app-sidebar";

export function renderSidebar(overrides: Partial<AppSidebarProps> = {}) {
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
