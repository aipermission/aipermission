import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Outlet, useLocation } from "react-router";
import { apiGet, apiPost, apiPut } from "../lib/api";
import { BackupFreshnessNotices } from "./backup-freshness-notices";
import { AppSidebar } from "./app-sidebar";
import { createPollGenerationGuard, liveConsoleRuntimeTargets, normalizeCredentialResources } from "./app-shell-runtime";
import { DatabaseSwitchDialog } from "./database-switch-dialog";
import { DatabaseLockDialog } from "./database-lock-dialog";
import { LocalActionReconciliationDialog } from "./local-action-reconciliation-dialog";
import { useLocalActionReconciliation } from "./use-local-action-reconciliation";
import { useTransferCenterState } from "./file-transfer/use-transfer-center-state";
import { TransferCenter } from "./transfer-center";
import { VaultSessionDialog } from "./console/vault-session-dialog";
import { VaultActionApprovalDialog } from "./vault/vault-action-approval-dialog";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { getConnectorModel } from "../connectors/templates/registry";
import { isUnreadMessage } from "./console/helpers";
import { useConsoleSessionCoordinator } from "./console/use-console-session-coordinator";
import { useDatabaseLifecycle } from "./use-database-lifecycle";
import { useVaultActionApprovals } from "./vault/use-vault-action-approvals";
export function Shell({ theme, setTheme }) {
  const location = useLocation();
  function toggleTheme() {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }
  const [status, setStatus] = useState({ state: "loading", data: null, error: null });
  const [targets, setTargets] = useState({ state: "loading", data: [], error: null });
  const [credentials, setCredentials] = useState({ state: "loading", data: [], error: null, errors: [] });
  const [tokens, setTokens] = useState({ state: "loading", data: [], error: null });
  const [connectorActionApprovals, setConnectorActionApprovals] = useState({ state: "loading", data: [], error: null });
  const [messages, setMessages] = useState({ state: "loading", data: [], error: null });
  const [mcpRuntime, setMCPRuntime] = useState({ state: "loading", data: { enabled: false, start_enabled: false }, error: null });
  const [backupFreshness, setBackupFreshness] = useState({ state: "loading", data: [], checkErrors: [], error: null });
  const [actionRetryDialog, closeActionRetryDialog] = useLocalActionReconciliation();
  const pollGenerationGuard = useRef(createPollGenerationGuard()).current;
  const pollIsCurrent = useCallback((generation) => pollGenerationGuard.isCurrent(generation), [pollGenerationGuard]);
  const transferCenter = useTransferCenterState({ pollIsCurrent });
  const consoleCoordinator = useConsoleSessionCoordinator({ pollIsCurrent });
  const database = useDatabaseLifecycle({ disconnectAllConsoleSessions: consoleCoordinator.disconnectAll, pollIsCurrent });
  const vaultApprovals = useVaultActionApprovals({ pollIsCurrent, refreshConsoleSessions: consoleCoordinator.loadSessions });

  async function loadStatus(generation) {
    try {
      const data = await apiGet("/api/status");
      if (!pollIsCurrent(generation)) return;
      setStatus({ state: "ready", data, error: null });
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setStatus({ state: "error", data: null, error: error.message });
    }
  }

  async function loadTargets(generation) {
    try {
      const data = await apiGet("/api/targets");
      if (!pollIsCurrent(generation)) return;
      setTargets({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  async function loadCredentials(generation) {
    try {
      const results = await Promise.allSettled(
        supportedConnectorKinds.map(async (kind) => {
          const model = getConnectorModel(kind);
          if (!model?.loadCredentialResources) return [];
          const items = await model.loadCredentialResources();
          return normalizeCredentialResources(kind, items);
        }),
      );
      const data = results.flatMap((result) => (result.status === "fulfilled" ? result.value : []));
      const errors = results
        .map((result, index) =>
          result.status === "rejected" ? `${supportedConnectorKinds[index]}: ${result.reason?.message || result.reason}` : "",
        )
        .filter(Boolean);
      if (!pollIsCurrent(generation)) return;
      setCredentials({ state: "ready", data, error: null, errors });
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setCredentials({ state: "error", data: [], error: error.message, errors: [] });
    }
  }

  async function loadTokens(generation) {
    try {
      const data = await apiGet("/api/tokens");
      if (!pollIsCurrent(generation)) return [];
      setTokens({ state: "ready", data, error: null });
      return data;
    } catch (error) {
      if (!pollIsCurrent(generation)) return [];
      setTokens({ state: "error", data: [], error: error.message });
      return [];
    }
  }

  async function loadConnectorActionApprovals(generation) {
    try {
      const data = await apiGet("/api/connector-action-approvals");
      if (!pollIsCurrent(generation)) return;
      setConnectorActionApprovals({ state: "ready", data, error: null });
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setConnectorActionApprovals({ state: "error", data: [], error: error.message });
    }
  }

  async function loadMessages(generation) {
    try {
      const data = await apiGet("/api/messages");
      if (!pollIsCurrent(generation)) return;
      setMessages({ state: "ready", data, error: null });
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setMessages({ state: "error", data: [], error: error.message });
    }
  }

  async function loadMCPRuntime(generation) {
    try {
      const data = await apiGet("/api/settings/mcp-runtime");
      if (!pollIsCurrent(generation)) return { enabled: false, start_enabled: false };
      setMCPRuntime({ state: "ready", data, error: null });
      return data;
    } catch (error) {
      if (!pollIsCurrent(generation)) return { enabled: false, start_enabled: false };
      setMCPRuntime({ state: "error", data: { enabled: false, start_enabled: false }, error: error.message });
      return { enabled: false, start_enabled: false };
    }
  }

  async function loadBackupFreshness() {
    try {
      const data = await apiGet("/api/backup/freshness");
      setBackupFreshness({ state: "ready", data: data?.items || [], checkErrors: data?.check_errors || [], error: null });
    } catch (error) {
      setBackupFreshness({ state: "error", data: [], checkErrors: [], error: error.message });
    }
  }

  async function refreshAll(generation) {
    await Promise.all([
      loadStatus(generation),
      database.loadStatus(generation),
      loadMCPRuntime(generation),
      loadTargets(generation),
      loadCredentials(generation),
      loadTokens(generation),
      consoleCoordinator.loadSessions(generation),
      loadConnectorActionApprovals(generation),
      vaultApprovals.load(generation),
      loadMessages(generation),
      transferCenter.loadBatches({ keepData: true }, generation),
    ]);
  }

  const refreshCurrentRoute = useEffectEvent(async (pathname, firstLoad, generation) => {
    if (firstLoad || pathname !== "/console") {
      await refreshAll(generation);
      return;
    }
    await Promise.all([
      loadStatus(generation),
      database.loadStatus(generation),
      loadTargets(generation),
      consoleCoordinator.loadSessions(generation),
      loadConnectorActionApprovals(generation),
      vaultApprovals.load(generation),
      loadMessages(generation),
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
  }, []);

  useEffect(() => {
    const unlocked = database.status.data?.unlocked === true || database.status.data?.state === "unlocked";
    if (database.status.state !== "ready" || !unlocked) {
      document.title = "AIPermission";
      return;
    }
    const runtimeLabel = mcpRuntime.data?.enabled ? "Started" : "Stopped";
    const databaseName = database.status.data?.database_name || database.status.data?.database_id || "Database";
    document.title = `${runtimeLabel} - ${databaseName}`;
  }, [
    database.status.state,
    database.status.data?.unlocked,
    database.status.data?.state,
    database.status.data?.database_name,
    database.status.data?.database_id,
    mcpRuntime.data?.enabled,
  ]);

  const gatewayState = useMemo(() => {
    if (status.state === "ready") return "running";
    if (status.state === "error") return "unreachable";
    return "checking";
  }, [status.state]);
  const liveConsoleTargets = useMemo(() => {
    if (targets.state === "error") {
      return { state: "error", data: [], error: targets.error };
    }
    if (targets.state === "loading") {
      return { state: "loading", data: [], error: null };
    }
    return { state: "ready", data: liveConsoleRuntimeTargets(targets.data, getConnectorModel), error: null };
  }, [targets.state, targets.data, targets.error]);

  async function runConnectorActionApproval(requestID, userNote = "") {
    try {
      const item = await apiPost(`/api/connector-action-approvals/${requestID}/run`, { user_note: userNote });
      await loadConnectorActionApprovals();
      return item;
    } catch (error) {
      await loadConnectorActionApprovals();
      throw error;
    }
  }

  async function declineConnectorActionApproval(requestID, userNote = "") {
    const item = await apiPost(`/api/connector-action-approvals/${requestID}/decline`, { user_note: userNote });
    await loadConnectorActionApprovals();
    return item;
  }

  async function markRuntimeMessagesRead(runtimeID) {
    const result = await apiPost("/api/messages/read", { runtime_id: Number(runtimeID) });
    await loadMessages();
    return result;
  }

  async function setMCPRuntimeEnabled(enabled) {
    const data = await apiPut("/api/settings/mcp-runtime", { enabled });
    setMCPRuntime({ state: "ready", data, error: null });
    return data;
  }

  const pendingConnectorActionApprovalCount = connectorActionApprovals.data.filter(
    (approval) => approval.status === "approval_pending",
  ).length;
  const pendingVaultActionApprovalCount = vaultApprovals.approvals.data.filter((approval) => approval.status === "approval_pending").length;
  const unreadMessageCount = messages.data.filter(isUnreadMessage).length;
  const consoleAttentionCount = pendingConnectorActionApprovalCount + pendingVaultActionApprovalCount + unreadMessageCount;

  return (
    <main className="min-h-screen bg-stone-100 text-stone-950">
      <AppSidebar
        pathname={location.pathname}
        consoleAttentionCount={consoleAttentionCount}
        activeTransferCount={transferCenter.activeCount}
        gatewayState={gatewayState}
        mcpRuntime={mcpRuntime}
        theme={theme}
        onSetTheme={setTheme}
        onSetMCPRuntimeEnabled={setMCPRuntimeEnabled}
        onOpenTransferCenter={transferCenter.show}
        onSwitchDatabase={database.openSwitch}
        onLockDatabase={database.requestLock}
      />

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
          <BackupFreshnessNotices value={backupFreshness} onChange={setBackupFreshness} />
          <Outlet
            context={{
              status,
              liveConsoleTargets,
              targets,
              credentials,
              tokens,
              connectorActionApprovals,
              messages,
              mcpRuntime,
              loadStatus,
              loadTargets,
              loadCredentials,
              loadTokens,
              loadConnectorActionApprovals,
              loadMessages,
              markRuntimeMessagesRead,
              setMCPRuntimeEnabled,
              refreshAll,
              gatewayState,
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
              runConnectorActionApproval,
              declineConnectorActionApproval,
              theme,
              toggleTheme,
            }}
          />
        </div>
      </section>
    </main>
  );
}
