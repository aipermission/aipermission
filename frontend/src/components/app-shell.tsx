import { Outlet, useLocation } from "react-router";
import { BackupFreshnessNotices } from "./backup-freshness-notices";
import { AppSidebar } from "./app-sidebar";
import { AppMobileNavigation } from "./app-mobile-navigation";
import { DatabaseSwitchDialog } from "./database-switch-dialog";
import { DatabaseLockDialog } from "./database-lock-dialog";
import { LocalActionReconciliationDialog } from "./local-action-reconciliation-dialog";
import { TransferCenter } from "./transfer-center";
import { VaultSessionDialog } from "./console/vault-session-dialog";
import { VaultActionApprovalDialog } from "./vault/vault-action-approval-dialog";
import { PendingVaultApprovalNotice } from "./vault/pending-vault-approval-notice";
import { useAppShellController } from "./use-app-shell-controller";
import type { Dispatch, SetStateAction } from "react";
import type { Theme } from "../lib/theme.ts";
export function Shell({ theme, setTheme }: { theme: Theme; setTheme: Dispatch<SetStateAction<Theme>> }) {
  const location = useLocation();
  const {
    resources,
    transferCenter,
    consoleCoordinator,
    database,
    vaultApprovals,
    actionRetryDialog,
    closeActionRetryDialog,
    pendingVaultActionApprovalCount,
    consoleAttentionCount,
    gatewayContext,
  } = useAppShellController({ pathname: location.pathname, theme, setTheme });
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
          {vaultApprovals.approvals.state === "ready" && pendingVaultActionApprovalCount > 0 && !vaultApprovals.dialog.approval ? (
            <PendingVaultApprovalNotice count={pendingVaultActionApprovalCount} onReview={vaultApprovals.openPending} />
          ) : null}
          <Outlet context={gatewayContext} />
        </div>
      </section>
    </main>
  );
}
