import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Shell } from "./app-shell";
import type { VaultApproval } from "../lib/gateway-contracts/security-contracts.ts";
import type { GatewayContext } from "../lib/gateway-context";

type FixtureState = {
  pathname: string;
  resources: ReturnType<typeof gatewayResources> | null;
  database: ReturnType<typeof databaseState> | null;
  transfers: ReturnType<typeof transferState> | null;
  console: ReturnType<typeof consoleState> | null;
  vault: ReturnType<typeof vaultState> | null;
};
const state = vi.hoisted<FixtureState>(() => ({ pathname: "/console", resources: null, database: null, transfers: null, console: null, vault: null }));

vi.mock("react-router", () => ({
  useLocation: () => ({ pathname: state.pathname }),
  Outlet: ({ context }: { context: GatewayContext }) => (
    <div data-testid="outlet">
      <button type="button" onClick={context.toggleTheme}>
        Toggle test route theme
      </button>
    </div>
  ),
}));
vi.mock("./app-sidebar", () => ({ AppSidebar: () => null }));
vi.mock("./backup-freshness-notices", () => ({ BackupFreshnessNotices: () => null }));
vi.mock("./database-switch-dialog", () => ({ DatabaseSwitchDialog: () => null }));
vi.mock("./database-lock-dialog", () => ({ DatabaseLockDialog: () => null }));
vi.mock("./local-action-reconciliation-dialog", () => ({ LocalActionReconciliationDialog: () => null }));
vi.mock("./transfer-center", () => ({ TransferCenter: () => null }));
vi.mock("./console/vault-session-dialog", () => ({ VaultSessionDialog: () => null }));
vi.mock("./vault/vault-action-approval-dialog", () => ({ VaultActionApprovalDialog: () => null }));
vi.mock("./use-local-action-reconciliation", () => ({ useLocalActionReconciliation: () => [{ open: false }, vi.fn()] }));
vi.mock("./use-gateway-resources", () => ({ useGatewayResources: () => state.resources }));
vi.mock("./use-database-lifecycle", () => ({ useDatabaseLifecycle: () => state.database }));
vi.mock("./file-transfer/use-transfer-center-state", () => ({ useTransferCenterState: () => state.transfers }));
vi.mock("./console/use-console-session-coordinator", () => ({ useConsoleSessionCoordinator: () => state.console }));
vi.mock("./vault/use-vault-action-approvals", () => ({ useVaultActionApprovals: () => state.vault }));

beforeEach(() => {
  vi.useFakeTimers();
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

function asyncMock() {
  return vi.fn(async () => {});
}

function gatewayResources() {
  return {
    status: { state: "ready", data: {} },
    targets: { state: "ready", data: [] },
    credentials: { state: "ready", data: [] },
    tokens: { state: "ready", data: [] },
    connectorActionApprovals: { data: [] },
    messages: { data: [] },
    mcpRuntime: { data: { enabled: false } },
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
    status: { state: "ready", data: { state: "unlocked", database_name: "Default" } },
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
    loadBatches: asyncMock(),
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
