import { useCallback, useEffect, useEffectEvent, useRef } from "react";
import { Outlet, useLocation } from "react-router";
import { BackupFreshnessNotices } from "./backup-freshness-notices";
import { AppSidebar } from "./app-sidebar";
import { AppMobileNavigation } from "./app-mobile-navigation";
import { createPollGenerationGuard } from "./app-shell-runtime";
import { DatabaseSwitchDialog } from "./database-switch-dialog";
import { DatabaseLockDialog } from "./database-lock-dialog";
import { LocalActionReconciliationDialog } from "./local-action-reconciliation-dialog";
import { useLocalActionReconciliation } from "./use-local-action-reconciliation";
import { useTransferCenterState } from "./file-transfer/use-transfer-center-state";
import { TransferCenter } from "./transfer-center";
import { VaultSessionDialog } from "./console/vault-session-dialog";
import { VaultActionApprovalDialog } from "./vault/vault-action-approval-dialog";
import { isUnreadMessage } from "./console/helpers";
import { useConsoleSessionCoordinator } from "./console/use-console-session-coordinator";
import { useDatabaseLifecycle } from "./use-database-lifecycle";
import { useGatewayResources } from "./use-gateway-resources";
import { useVaultActionApprovals } from "./vault/use-vault-action-approvals";
export function Shell({ theme, setTheme }) {
  const location = useLocation();
  function toggleTheme() {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }
  const [actionRetryDialog, closeActionRetryDialog] = useLocalActionReconciliation();
  const pollGenerationGuard = useRef(createPollGenerationGuard()).current;
  const pollIsCurrent = useCallback((generation) => pollGenerationGuard.isCurrent(generation), [pollGenerationGuard]);
  const resources = useGatewayResources({ pollIsCurrent });
  const { loadBackupFreshness } = resources;
  const transferCenter = useTransferCenterState({ pollIsCurrent });
  const consoleCoordinator = useConsoleSessionCoordinator({ pollIsCurrent });
  const database = useDatabaseLifecycle({ disconnectAllConsoleSessions: consoleCoordinator.disconnectAll, pollIsCurrent });
  const vaultApprovals = useVaultActionApprovals({ pollIsCurrent, refreshConsoleSessions: consoleCoordinator.loadSessions });

  async function refreshAll(generation) {
    await Promise.allSettled([
      resources.loadStatus(generation),
      database.loadStatus(generation),
      resources.loadMCPRuntime(generation),
      resources.loadTargets(generation),
      resources.loadCredentials(generation),
      resources.loadTokens(generation),
      consoleCoordinator.loadSessions(generation),
      resources.loadConnectorActionApprovals(generation),
      vaultApprovals.load(generation),
      resources.loadMessages(generation),
      transferCenter.loadBatches({ keepData: true }, generation),
    ]);
  }

  const refreshCurrentRoute = useEffectEvent(async (pathname, firstLoad, generation) => {
    if (firstLoad || pathname !== "/console") {
      await refreshAll(generation);
      return;
    }
    await Promise.allSettled([
      resources.loadStatus(generation),
      database.loadStatus(generation),
      resources.loadTargets(generation),
      consoleCoordinator.loadSessions(generation),
      resources.loadConnectorActionApprovals(generation),
      vaultApprovals.load(generation),
      resources.loadMessages(generation),
      transferCenter.loadBatches({ keepData: true }, generation),
    ]);
  });

  useEffect(() => {
    let cancelled = false;
    let firstLoad = true;
    let timer = 0;
    const generation = pollGenerationGuard.begin();
    async function load() {
      if (cancelled) return;
      const initial = firstLoad;
      firstLoad = false;
      await refreshCurrentRoute(location.pathname, initial, generation);
      if (!cancelled) timer = window.setTimeout(load, 5000);
    }
    void load();
    return () => {
      cancelled = true;
      pollGenerationGuard.invalidate();
      window.clearTimeout(timer);
    };
  }, [location.pathname, pollGenerationGuard]);

  useEffect(() => {
    void loadBackupFreshness();
  }, [loadBackupFreshness]);

  useEffect(() => {
    const unlocked = database.status.data?.unlocked === true || database.status.data?.state === "unlocked";
    if (database.status.state !== "ready" || !unlocked) {
      document.title = "AIPermission";
      return;
    }
    const runtimeLabel = resources.mcpRuntime.data?.enabled ? "Started" : "Stopped";
    const databaseName = database.status.data?.database_name || database.status.data?.database_id || "Database";
    document.title = `${runtimeLabel} - ${databaseName}`;
  }, [
    database.status.state,
    database.status.data?.unlocked,
    database.status.data?.state,
    database.status.data?.database_name,
    database.status.data?.database_id,
    resources.mcpRuntime.data?.enabled,
  ]);

  const pendingConnectorActionApprovalCount = resources.connectorActionApprovals.data.filter(
    (approval) => approval.status === "approval_pending",
  ).length;
  const pendingVaultActionApprovalCount = vaultApprovals.approvals.data.filter((approval) => approval.status === "approval_pending").length;
  const unreadMessageCount = resources.messages.data.filter(isUnreadMessage).length;
  const consoleAttentionCount = pendingConnectorActionApprovalCount + pendingVaultActionApprovalCount + unreadMessageCount;
  const sidebarProps = {
    pathname: location.pathname,
    consoleAttentionCount,
    activeTransferCount: transferCenter.activeCount,
    gatewayState: resources.gatewayState,
    mcpRuntime: resources.mcpRuntime,
    theme,
    onSetTheme: setTheme,
    onSetMCPRuntimeEnabled: resources.setMCPRuntimeEnabled,
    onOpenTransferCenter: transferCenter.show,
    onSwitchDatabase: database.openSwitch,
    onLockDatabase: database.requestLock,
  };

  return (
    <main className="min-h-screen bg-stone-100 text-stone-950">
      <AppSidebar {...sidebarProps} />
      <AppMobileNavigation sidebarProps={sidebarProps} />

      <TransferCenter
        open={transferCenter.open}
        batches={transferCenter.batches.data}
        state={transferCenter.batches.state}
        error={transferCenter.batches.error}
        onClose={transferCenter.close}
        onRefresh={() => transferCenter.loadBatches({ keepData: true })}
        onPause={transferCenter.actions.pause}
        onResume={transferCenter.actions.resume}
        onCancel={transferCenter.actions.cancel}
        onApprove={transferCenter.actions.approve}
        onDecline={transferCenter.actions.decline}
      />
      <VaultSessionDialog
        state={consoleCoordinator.vaultDialog}
        onClose={consoleCoordinator.closeVaultDialog}
        onStart={consoleCoordinator.startVaultSession}
      />
      <VaultActionApprovalDialog
        approval={vaultApprovals.dialog.approval}
        note={vaultApprovals.dialog.note}
        action={vaultApprovals.dialog}
        onNoteChange={vaultApprovals.setNote}
        onRun={vaultApprovals.run}
        onDecline={vaultApprovals.decline}
        onClose={vaultApprovals.close}
      />

      <DatabaseSwitchDialog
        state={database.switchDialog}
        databaseStatus={database.status.data}
        onChange={database.setSwitchDialog}
        onClose={database.closeSwitch}
        onSubmit={database.switchDatabase}
      />

      <DatabaseLockDialog state={database.lockDialog} onClose={database.closeLock} onLock={database.lock} />

      <LocalActionReconciliationDialog value={actionRetryDialog} onClose={closeActionRetryDialog} />

      <section className="lg:pl-72">
        <div className={`mx-auto grid gap-6 p-5 ${location.pathname === "/console" ? "max-w-none" : "max-w-7xl"}`}>
          <BackupFreshnessNotices value={resources.backupFreshness} onChange={resources.setBackupFreshness} />
          <Outlet
            context={{
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
              refreshAll,
              gatewayState: resources.gatewayState,
              consoleSessions: consoleCoordinator.sessions,
              loadConsoleSessions: consoleCoordinator.loadSessions,
              ensureConsoleSession: consoleCoordinator.ensureSession,
              newConsoleSession: consoleCoordinator.newSession,
              attachConsoleSession: consoleCoordinator.attachSession,
              closeConsoleSession: consoleCoordinator.closeSession,
              cancelConsoleCommand: consoleCoordinator.cancelCommand,
              restartConsoleRuntime: consoleCoordinator.restartRuntime,
              sendConsoleInput: consoleCoordinator.sendInput,
              resizeConsoleSession: consoleCoordinator.resizeSession,
              runConnectorActionApproval: resources.runConnectorActionApproval,
              declineConnectorActionApproval: resources.declineConnectorActionApproval,
              theme,
              toggleTheme,
            }}
          />
        </div>
      </section>
    </main>
  );
}
