import { vi } from "vitest";
import type { ConsoleWorkspaceSlotProps } from "../components/console/console-workspace-types";
import { gatewayTargetFixture } from "./connector-inventory-fixtures";

export function consoleWorkspaceFixture(overrides: Partial<ConsoleWorkspaceSlotProps> = {}): ConsoleWorkspaceSlotProps {
  return {
    target: gatewayTargetFixture(),
    session: null,
    approvals: { state: "ready", data: [], error: null },
    theme: "dark",
    selectedSessionLive: false,
    selectedRuntimeTarget: null,
    onNewStructuredSession: vi.fn(),
    onNewLiveSession: vi.fn(),
    onSelectLiveSessionName: vi.fn(),
    onEndLiveSession: vi.fn(),
    onOpenActivity: vi.fn(),
    onRefreshActivity: vi.fn(),
    ...overrides,
  };
}
