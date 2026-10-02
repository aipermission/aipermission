import { useCallback, useEffect, useEffectEvent, useRef } from "react";
import { createPollGenerationGuard } from "./app-shell-runtime";
import { useLocalActionReconciliation } from "./use-local-action-reconciliation";
import { useTransferCenterState } from "./file-transfer/use-transfer-center-state";
import { isUnreadMessage } from "./console/helpers";
import { useConsoleSessionCoordinator } from "./console/use-console-session-coordinator";
import { useDatabaseLifecycle } from "./use-database-lifecycle";
import { useGatewayResources } from "./use-gateway-resources";
import { useVaultActionApprovals } from "./vault/use-vault-action-approvals";
import type { Dispatch, SetStateAction } from "react";
import type { Theme } from "../lib/theme.ts";
import type { GatewayContext } from "../lib/gateway-context.ts";

export function useAppShellController({
  pathname,
  theme,
  setTheme,
}: {
  pathname: string;
  theme: Theme;
  setTheme: Dispatch<SetStateAction<Theme>>;
}) {
  function toggleTheme() {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }
  const [actionRetryDialog, closeActionRetryDialog] = useLocalActionReconciliation();
  const pollGenerationGuard = useRef(createPollGenerationGuard()).current;
  const pollIsCurrent = useCallback((generation?: number) => pollGenerationGuard.isCurrent(generation), [pollGenerationGuard]);
  const resources = useGatewayResources({ pollIsCurrent });
  // Backup notices and runtime polling belong to the Shell, not route context.
  const {
    backupFreshness: _backupFreshness,
    setBackupFreshness: _setBackupFreshness,
    loadBackupFreshness,
    loadMCPRuntime,
    ...gatewayResources
  } = resources;
  const transferCenter = useTransferCenterState({ pollIsCurrent });
  const consoleCoordinator = useConsoleSessionCoordinator({ pollIsCurrent });
  const database = useDatabaseLifecycle({ disconnectAllConsoleSessions: consoleCoordinator.disconnectAll, pollIsCurrent });
  const vaultApprovals = useVaultActionApprovals({ pollIsCurrent, refreshConsoleSessions: consoleCoordinator.loadSessions });

  async function refreshAll(generation?: number, includeColdResources = true) {
    const requests: Promise<unknown>[] = [
      resources.loadStatus(generation),
      database.loadStatus(generation),
      resources.loadTargets(generation),
      consoleCoordinator.loadSessions(generation),
      resources.loadConnectorActionApprovals(generation),
      vaultApprovals.load(generation),
      resources.loadMessages(generation),
      transferCenter.loadBatches({ keepData: true }, generation),
    ];
    if (includeColdResources) {
      requests.push(loadMCPRuntime(generation), resources.loadCredentials(generation), resources.loadTokens(generation));
    }
    await Promise.allSettled(requests);
  }

  const refreshCurrentRoute = useEffectEvent((pathname: string, firstLoad: boolean, generation: number) =>
    refreshAll(generation, firstLoad || pathname !== "/console"),
  );

  useEffect(() => {
    let cancelled = false;
    let firstLoad = true;
    let timer = 0;
    const generation = pollGenerationGuard.begin();
    async function load() {
      if (cancelled) return;
      const initial = firstLoad;
      firstLoad = false;
      await refreshCurrentRoute(pathname, initial, generation);
      if (!cancelled) timer = window.setTimeout(load, 5000);
    }
    void load();
    return () => {
      cancelled = true;
      pollGenerationGuard.invalidate();
      window.clearTimeout(timer);
    };
  }, [pathname, pollGenerationGuard]);

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
  const gatewayContext = {
    ...gatewayResources,
    refreshAll,
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
    theme,
    toggleTheme,
  } satisfies GatewayContext & Record<Exclude<keyof typeof gatewayResources, keyof GatewayContext>, never>;

  return {
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
  };
}
