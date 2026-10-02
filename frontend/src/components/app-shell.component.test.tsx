import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Shell } from "./app-shell";
import type { VaultApproval } from "../lib/gateway-contracts/security-contracts.ts";
import type { GatewayContext } from "../lib/gateway-context";
import type { ComponentProps } from "react";
import type { AppSidebar } from "./app-sidebar";
import type { TransferCenter } from "./transfer-center";

type FixtureState = {
  pathname: string;
  resources: ReturnType<typeof gatewayResources> | null;
  database: ReturnType<typeof databaseState> | null;
  transfers: ReturnType<typeof transferState> | null;
  console: ReturnType<typeof consoleState> | null;
  vault: ReturnType<typeof vaultState> | null;
  context: GatewayContext | null;
  sidebar: ComponentProps<typeof AppSidebar> | null;
  transferProps: ComponentProps<typeof TransferCenter> | null;
};
const state = vi.hoisted<FixtureState>(() => ({
  pathname: "/console",
  resources: null,
  database: null,
  transfers: null,
  console: null,
  vault: null,
  context: null,
  sidebar: null,
  transferProps: null,
}));
const wiring = vi.hoisted(() => ({ resources: vi.fn(), database: vi.fn(), transfers: vi.fn(), console: vi.fn(), vault: vi.fn() }));

vi.mock("react-router", () => ({
  useLocation: () => ({ pathname: state.pathname }),
  Outlet: ({ context }: { context: GatewayContext }) => {
    state.context = context;
    return (
      <div data-testid="outlet">
        <button type="button" onClick={context.toggleTheme}>
          Toggle test route theme
        </button>
      </div>
    );
  },
}));
vi.mock("./app-sidebar", () => ({
  AppSidebar: (props: ComponentProps<typeof AppSidebar>) => {
    state.sidebar = props;
    return null;
  },
}));
vi.mock("./backup-freshness-notices", () => ({ BackupFreshnessNotices: () => null }));
vi.mock("./database-switch-dialog", () => ({ DatabaseSwitchDialog: () => null }));
vi.mock("./database-lock-dialog", () => ({ DatabaseLockDialog: () => null }));
vi.mock("./local-action-reconciliation-dialog", () => ({ LocalActionReconciliationDialog: () => null }));
vi.mock("./transfer-center", () => ({
  TransferCenter: (props: ComponentProps<typeof TransferCenter>) => {
    state.transferProps = props;
    return null;
  },
}));
vi.mock("./console/vault-session-dialog", () => ({ VaultSessionDialog: () => null }));
vi.mock("./vault/vault-action-approval-dialog", () => ({ VaultActionApprovalDialog: () => null }));
vi.mock("./use-local-action-reconciliation", () => ({ useLocalActionReconciliation: () => [{ open: false }, vi.fn()] }));
vi.mock("./use-gateway-resources", () => ({
  useGatewayResources: (options: unknown) => {
    wiring.resources(options);
    return state.resources;
  },
}));
vi.mock("./use-database-lifecycle", () => ({
  useDatabaseLifecycle: (options: unknown) => {
    wiring.database(options);
    return state.database;
  },
}));
vi.mock("./file-transfer/use-transfer-center-state", () => ({
  useTransferCenterState: (options: unknown) => {
    wiring.transfers(options);
    return state.transfers;
  },
}));
vi.mock("./console/use-console-session-coordinator", () => ({
  useConsoleSessionCoordinator: (options: unknown) => {
    wiring.console(options);
    return state.console;
  },
}));
vi.mock("./vault/use-vault-action-approvals", () => ({
  useVaultActionApprovals: (options: unknown) => {
    wiring.vault(options);
    return state.vault;
  },
}));

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  document.title = "AIPermission";
  state.pathname = "/console";
  state.resources = gatewayResources();
  state.database = databaseState();
  state.transfers = transferState();
  state.console = consoleState();
  state.vault = vaultState();
});

it("passes a reversible theme toggle through the native route context", () => {
  const setTheme = vi.fn();
  render(<Shell theme="dark" setTheme={setTheme} />);
  fireEvent.click(screen.getByRole("button", { name: "Toggle test route theme" }));
  expect(setTheme).toHaveBeenCalledOnce();
  const toggle = setTheme.mock.calls[0][0];
  expect(typeof toggle).toBe("function");
  expect(toggle("dark")).toBe("light");
  expect(toggle("light")).toBe("dark");
});

afterEach(() => {
  vi.useRealTimers();
});

it("serializes route polling and stops scheduling after unmount", async () => {
  const view = render(<Shell theme="dark" setTheme={vi.fn()} />);
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  expect(state.resources?.loadStatus).toHaveBeenCalledOnce();

  await act(async () => vi.advanceTimersByTimeAsync(5000));
  expect(state.resources?.loadStatus).toHaveBeenCalledTimes(2);

  view.unmount();
  await act(async () => vi.advanceTimersByTimeAsync(10000));
  expect(state.resources?.loadStatus).toHaveBeenCalledTimes(2);
});

it("offers a way back to a dismissed pending Vault approval", () => {
  if (!state.vault) throw new Error("Vault fixture not initialized");
  state.vault.approvals = { state: "ready", data: [{ id: 42, status: "approval_pending" }] };
  state.vault.openPending = vi.fn();
  render(<Shell theme="dark" setTheme={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: "Review pending Vault approval" }));
  expect(state.vault.openPending).toHaveBeenCalledOnce();
});

it("preserves the exact route context and resource callback identities", async () => {
  render(<Shell theme="dark" setTheme={vi.fn()} />);
  const resources = state.resources!;
  const coordinator = state.console!;
  const expected = {
    status: resources.status,
    liveConsoleTargets: resources.liveConsoleTargets,
    targets: resources.targets,
    credentials: resources.credentials,
    tokens: resources.tokens,
    connectorActionApprovals: resources.connectorActionApprovals,
    messages: resources.messages,
    mcpRuntime: resources.mcpRuntime,
    loadStatus: resources.loadStatus,
    loadTargets: resources.loadTargets,
    loadCredentials: resources.loadCredentials,
    loadTokens: resources.loadTokens,
    loadConnectorActionApprovals: resources.loadConnectorActionApprovals,
    loadMessages: resources.loadMessages,
    markRuntimeMessagesRead: resources.markRuntimeMessagesRead,
    setMCPRuntimeEnabled: resources.setMCPRuntimeEnabled,
    gatewayState: resources.gatewayState,
    runConnectorActionApproval: resources.runConnectorActionApproval,
    declineConnectorActionApproval: resources.declineConnectorActionApproval,
    consoleSessions: coordinator.sessions,
    loadConsoleSessions: coordinator.loadSessions,
    ensureConsoleSession: coordinator.ensureSession,
    newConsoleSession: coordinator.newSession,
    attachConsoleSession: coordinator.attachSession,
    closeConsoleSession: coordinator.closeSession,
    cancelConsoleCommand: coordinator.cancelCommand,
    restartConsoleRuntime: coordinator.restartRuntime,
    sendConsoleInput: coordinator.sendInput,
    resizeConsoleSession: coordinator.resizeSession,
    theme: "dark",
  };
  expect(Object.keys(state.context!).sort()).toEqual([...Object.keys(expected), "refreshAll", "toggleTheme"].sort());
  for (const [key, value] of Object.entries(expected)) {
    expect(state.context![key as keyof GatewayContext]).toBe(value);
  }
  await act(async () => state.context!.refreshAll());
  expect(resources.loadTokens).toHaveBeenLastCalledWith(undefined);
  expect(state.transfers!.loadBatches).toHaveBeenLastCalledWith({ keepData: true }, undefined);
  await act(async () => state.context!.refreshAll(73));
  expect(resources.loadTokens).toHaveBeenLastCalledWith(73);
  expect(state.transfers!.loadBatches).toHaveBeenLastCalledWith({ keepData: true }, 73);
  await act(async () => state.transferProps!.onRefresh!());
  expect(state.transfers!.loadBatches).toHaveBeenLastCalledWith({ keepData: true });
});

it("uses full initial polling, reduced console polling, and full polling on other routes", async () => {
  const view = render(<Shell theme="dark" setTheme={vi.fn()} />);
  await act(async () => {});
  const resources = state.resources!;
  expect(resources.loadTokens).toHaveBeenCalledOnce();
  expect(resources.loadCredentials).toHaveBeenCalledOnce();
  expect(resources.loadMCPRuntime).toHaveBeenCalledOnce();
  await act(async () => vi.advanceTimersByTimeAsync(4999));
  expect(resources.loadStatus).toHaveBeenCalledOnce();
  await act(async () => vi.advanceTimersByTimeAsync(1));
  expect(resources.loadStatus).toHaveBeenCalledTimes(2);
  expect(resources.loadTokens).toHaveBeenCalledOnce();
  expect(resources.loadCredentials).toHaveBeenCalledOnce();
  expect(resources.loadMCPRuntime).toHaveBeenCalledOnce();
  for (const loader of [
    state.database!.loadStatus,
    resources.loadTargets,
    state.console!.loadSessions,
    resources.loadConnectorActionApprovals,
    state.vault!.load,
    resources.loadMessages,
    state.transfers!.loadBatches,
  ]) {
    expect(loader).toHaveBeenCalledTimes(2);
  }
  state.pathname = "/tokens";
  view.rerender(<Shell theme="light" setTheme={vi.fn()} />);
  await act(async () => {});
  expect(resources.loadTokens).toHaveBeenCalledTimes(2);
  await act(async () => vi.advanceTimersByTimeAsync(5000));
  expect(resources.loadTokens).toHaveBeenCalledTimes(3);
});

it("shares generation guards, retires pending route polls, and waits for settlement before scheduling", async () => {
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  state.resources!.loadStatus.mockImplementationOnce(() => pending);
  const view = render(<Shell theme="dark" setTheme={vi.fn()} />);
  const pollIsCurrent = wiring.resources.mock.calls[0][0].pollIsCurrent as (_generation?: number) => boolean;
  expect(wiring.transfers).toHaveBeenCalledWith({ pollIsCurrent });
  expect(wiring.console).toHaveBeenCalledWith({ pollIsCurrent });
  expect(wiring.database).toHaveBeenCalledWith({ pollIsCurrent, disconnectAllConsoleSessions: state.console!.disconnectAll });
  expect(wiring.vault).toHaveBeenCalledWith({ pollIsCurrent, refreshConsoleSessions: state.console!.loadSessions });
  const first = state.resources!.loadStatus.mock.calls[0][0];
  expect(pollIsCurrent(first)).toBe(true);
  await act(async () => vi.advanceTimersByTimeAsync(12000));
  expect(state.resources!.loadStatus).toHaveBeenCalledOnce();
  expect(vi.getTimerCount()).toBe(0);
  state.pathname = "/settings";
  state.resources!.loadStatus.mockImplementationOnce(() => Promise.reject(new Error("poll failed")));
  view.rerender(<Shell theme="dark" setTheme={vi.fn()} />);
  await act(async () => {});
  const second = state.resources!.loadStatus.mock.calls[1][0];
  expect(second).not.toBe(first);
  expect(pollIsCurrent(first)).toBe(false);
  expect(pollIsCurrent(second)).toBe(true);
  expect(pollIsCurrent()).toBe(true);
  expect(vi.getTimerCount()).toBe(1);
  await act(async () => finish());
  expect(vi.getTimerCount()).toBe(1);
  await act(async () => vi.advanceTimersByTimeAsync(5000));
  expect(state.resources!.loadStatus).toHaveBeenCalledTimes(3);
  view.unmount();
  expect(pollIsCurrent(second)).toBe(false);
  expect(pollIsCurrent()).toBe(true);
  expect(vi.getTimerCount()).toBe(0);
});

it("does not restart polling on render and uses the latest resource callbacks on the next tick", async () => {
  const view = render(<Shell theme="dark" setTheme={vi.fn()} />);
  await act(async () => {});
  const previous = state.resources!;
  state.resources = gatewayResources();
  view.rerender(<Shell theme="light" setTheme={vi.fn()} />);
  expect(state.context!.theme).toBe("light");
  expect(state.resources.loadStatus).not.toHaveBeenCalled();
  expect(previous.loadBackupFreshness).toHaveBeenCalledOnce();
  expect(state.resources.loadBackupFreshness).toHaveBeenCalledOnce();
  await act(async () => vi.advanceTimersByTimeAsync(5000));
  expect(previous.loadStatus).toHaveBeenCalledOnce();
  expect(state.resources.loadStatus).toHaveBeenCalledOnce();
  expect(state.resources.loadTokens).not.toHaveBeenCalled();
});

it("does not schedule a pending initial poll after unmount", async () => {
  let finish!: () => void;
  state.resources!.loadStatus.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const view = render(<Shell theme="dark" setTheme={vi.fn()} />);
  const pollIsCurrent = wiring.resources.mock.calls[0][0].pollIsCurrent;
  const generation = state.resources!.loadStatus.mock.calls[0][0];
  view.unmount();
  expect(pollIsCurrent(generation)).toBe(false);
  await act(async () => finish());
  await act(async () => vi.advanceTimersByTimeAsync(10000));
  expect(state.resources!.loadStatus).toHaveBeenCalledOnce();
  expect(vi.getTimerCount()).toBe(0);
});

it("starts the five-second delay only after a slow poll settles", async () => {
  let finish!: () => void;
  state.resources!.loadStatus.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  render(<Shell theme="dark" setTheme={vi.fn()} />);
  await act(async () => vi.advanceTimersByTimeAsync(12000));
  expect(state.resources!.loadStatus).toHaveBeenCalledOnce();
  await act(async () => finish());
  expect(vi.getTimerCount()).toBe(1);
  await act(async () => vi.advanceTimersByTimeAsync(4999));
  expect(state.resources!.loadStatus).toHaveBeenCalledOnce();
  await act(async () => vi.advanceTimersByTimeAsync(1));
  expect(state.resources!.loadStatus).toHaveBeenCalledTimes(2);
});

it.each([
  ["ready", { unlocked: true, database_name: "Named" }, true, "Started - Named"],
  ["ready", { state: "unlocked", database_id: "db-2" }, false, "Stopped - db-2"],
  ["ready", { state: "unlocked" }, undefined, "Stopped - Database"],
  ["ready", { state: "locked" }, true, "AIPermission"],
  ["loading", { unlocked: true }, true, "AIPermission"],
  ["ready", undefined, true, "AIPermission"],
] as const)("derives the document title from database and runtime state (%s, %j, %s)", (status, data, enabled, title) => {
  state.database!.status = { state: status, data };
  state.resources!.mcpRuntime.data = enabled === undefined ? undefined : { enabled };
  const view = render(<Shell theme="dark" setTheme={vi.fn()} />);
  expect(document.title).toBe(title);
  state.database!.status = { state: "ready", data: { state: "locked" } };
  view.rerender(<Shell theme="dark" setTheme={vi.fn()} />);
  expect(document.title).toBe("AIPermission");
});

it("counts only pending approvals and unread inbound messages for navigation attention", () => {
  state.resources!.connectorActionApprovals.data = [{ status: "approval_pending" }, { status: "approved" }];
  state.vault!.approvals.data = [
    { id: 1, status: "approval_pending" },
    { id: 2, status: "completed" },
  ];
  state.resources!.messages.data = [
    { direction: "ai_to_user", consumed_at: null },
    { direction: "ai_to_user", consumed_at: "already-read" },
    { direction: "user_to_ai" },
  ];
  state.transfers!.activeCount = 4;
  render(<Shell theme="dark" setTheme={vi.fn()} />);
  expect(state.sidebar!.consoleAttentionCount).toBe(3);
  expect(state.sidebar!.activeTransferCount).toBe(4);
  expect(state.sidebar!.onSwitchDatabase).toBe(state.database!.openSwitch);
  expect(state.sidebar!.onLockDatabase).toBe(state.database!.requestLock);
  expect(state.sidebar!.onSetMCPRuntimeEnabled).toBe(state.resources!.setMCPRuntimeEnabled);
  expect(state.sidebar!.onOpenTransferCenter).toBe(state.transfers!.show);
});

function asyncMock() {
  return vi.fn(async (_generation?: number) => {});
}

function gatewayResources() {
  return {
    status: { state: "ready", data: {} },
    targets: { state: "ready", data: [] },
    credentials: { state: "ready", data: [] },
    tokens: { state: "ready", data: [] },
    connectorActionApprovals: { data: [] as { status: string }[] },
    messages: { data: [] as { direction: string; consumed_at?: string | null }[] },
    mcpRuntime: { data: { enabled: false } as { enabled?: boolean } | undefined },
    backupFreshness: {},
    gatewayState: "running",
    liveConsoleTargets: [],
    loadStatus: asyncMock(),
    loadMCPRuntime: asyncMock(),
    loadTargets: asyncMock(),
    loadCredentials: asyncMock(),
    loadTokens: asyncMock(),
    loadConnectorActionApprovals: asyncMock(),
    loadMessages: asyncMock(),
    loadBackupFreshness: asyncMock(),
    setBackupFreshness: vi.fn(),
    setMCPRuntimeEnabled: vi.fn(),
    markRuntimeMessagesRead: vi.fn(),
    runConnectorActionApproval: vi.fn(),
    declineConnectorActionApproval: vi.fn(),
  };
}

function databaseState() {
  return {
    status: {
      state: "ready",
      data: { state: "unlocked", database_name: "Default" } as
        { state?: string; unlocked?: boolean; database_name?: string; database_id?: string } | undefined,
    },
    switchDialog: {},
    lockDialog: {},
    loadStatus: asyncMock(),
    openSwitch: vi.fn(),
    requestLock: vi.fn(),
    closeSwitch: vi.fn(),
    setSwitchDialog: vi.fn(),
    switchDatabase: vi.fn(),
    closeLock: vi.fn(),
    lock: vi.fn(),
  };
}

function transferState() {
  return {
    open: false,
    activeCount: 0,
    batches: { state: "ready", data: [], error: null },
    actions: { pause: vi.fn(), resume: vi.fn(), cancel: vi.fn(), approve: vi.fn(), decline: vi.fn() },
    loadBatches: vi.fn(async (_options?: { keepData: boolean }, _generation?: number) => {}),
    show: vi.fn(),
    close: vi.fn(),
  };
}

function consoleState() {
  return {
    sessions: { state: "ready", data: [] },
    vaultDialog: {},
    loadSessions: asyncMock(),
    disconnectAll: vi.fn(),
    closeVaultDialog: vi.fn(),
    startVaultSession: vi.fn(),
    ensureSession: vi.fn(),
    newSession: vi.fn(),
    attachSession: vi.fn(),
    closeSession: vi.fn(),
    cancelCommand: vi.fn(),
    restartRuntime: vi.fn(),
    sendInput: vi.fn(),
    resizeSession: vi.fn(),
  };
}

function vaultState() {
  const data: Pick<VaultApproval, "id" | "status">[] = [];
  return {
    approvals: { state: "ready", data },
    dialog: {},
    load: asyncMock(),
    setNote: vi.fn(),
    run: vi.fn(),
    decline: vi.fn(),
    close: vi.fn(),
    openPending: vi.fn(),
  };
}
