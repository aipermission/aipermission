import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Outlet, useLocation } from "react-router";
import { apiGet, apiPost, apiPut } from "../lib/api";
import { BackupFreshnessNotices } from "./backup-freshness-notices";
import { AppSidebar } from "./app-sidebar";
import {
  createPollGenerationGuard,
  isActiveTransferBatch,
  liveConsoleRuntimeTargets,
  mergeConsoleSessionData,
  normalizeCredentialResources,
} from "./app-shell-runtime";
import { DatabaseSwitchDialog } from "./database-switch-dialog";
import { createFileTransferBatchActions } from "./file-transfer/file-transfer-actions";
import { createFileTransferListState, loadCurrentFileTransferBatches } from "./file-transfer/file-transfer-list-state";
import { TransferCenter } from "./transfer-center";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Notice } from "./ui/notice";
import { VaultSessionDialog } from "./console/vault-session-dialog";
import { VaultActionApprovalDialog } from "./vault/vault-action-approval-dialog";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { getConnectorModel } from "../connectors/templates/registry";
import { reconcileVaultApprovalDialog } from "../lib/vault-approval-poll";
import { isLiveConsoleSession, isUnreadMessage, latestSessionForRuntime } from "./console/helpers";
import { useConsoleConnections } from "./console/use-console-connections";
export function Shell({ theme, setTheme }) {
  const location = useLocation();
  function toggleTheme() {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }
  const [status, setStatus] = useState({ state: "loading", data: null, error: null });
  const [targets, setTargets] = useState({ state: "loading", data: [], error: null });
  const [credentials, setCredentials] = useState({ state: "loading", data: [], error: null, errors: [] });
  const [tokens, setTokens] = useState({ state: "loading", data: [], error: null });
  const [consoleSessions, setConsoleSessions] = useState({ state: "loading", data: [], error: null });
  const [connectorActionApprovals, setConnectorActionApprovals] = useState({ state: "loading", data: [], error: null });
  const [vaultActionApprovals, setVaultActionApprovals] = useState({ state: "loading", data: [], error: null });
  const [messages, setMessages] = useState({ state: "loading", data: [], error: null });
  const [fileTransferBatches, setFileTransferBatches] = useState({ state: "loading", data: [], error: null });
  const [databaseStatus, setDatabaseStatus] = useState({ state: "loading", data: null, error: null });
  const [mcpRuntime, setMCPRuntime] = useState({ state: "loading", data: { enabled: false, start_enabled: false }, error: null });
  const [backupFreshness, setBackupFreshness] = useState({ state: "loading", data: [], checkErrors: [], error: null });
  const [switchDialog, setSwitchDialog] = useState({ open: false, database_id: "", password: "", state: "idle", error: null });
  const [lockDialog, setLockDialog] = useState({ open: false, state: "idle", error: null });
  const [transferCenterOpen, setTransferCenterOpen] = useState(false);
  const [vaultSessionDialog, setVaultSessionDialog] = useState({
    open: false,
    status: "idle",
    runtime: null,
    options: null,
    sessionOptions: null,
    error: null,
  });
  const [vaultActionDialog, setVaultActionDialog] = useState({ approval: null, note: "", state: "idle", error: null });
  const vaultSessionResolverRef = useRef(null);
  const seenPendingTransferApprovalsRef = useRef(new Set());
  const seenPendingVaultApprovalsRef = useRef(new Set());
  const fileTransferListState = useRef(createFileTransferListState()).current;
  const pollGenerationGuard = useRef(createPollGenerationGuard()).current;
  const {
    attachSession: attachConsoleSession,
    closeSession: closeConsoleSession,
    disconnectAll: disconnectAllConsoleSessions,
    disconnectSessions: disconnectConsoleSessions,
    resizeSession: resizeConsoleSession,
    sendInput: sendConsoleInput,
  } = useConsoleConnections({ setConsoleSessions });

  function pollIsCurrent(generation) {
    return pollGenerationGuard.isCurrent(generation);
  }

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

  async function loadDatabaseStatus(generation) {
    try {
      const data = await apiGet("/api/unlock/status");
      if (!pollIsCurrent(generation)) return;
      setDatabaseStatus({ state: "ready", data, error: null });
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setDatabaseStatus({ state: "error", data: null, error: error.message });
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

  async function loadConsoleSessions(generation) {
    try {
      const data = await apiGet("/api/console/sessions");
      if (!pollIsCurrent(generation)) return;
      setConsoleSessions((current) => ({ state: "ready", data: mergeConsoleSessionData(data, current.data), error: null }));
      data.filter((session) => isLiveConsoleSession(session)).forEach((session) => attachConsoleSession(session.id));
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setConsoleSessions({ state: "error", data: [], error: error.message });
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

  async function loadVaultActionApprovals(generation) {
    try {
      const data = await apiGet("/api/vault-action-approvals?status=approval_pending");
      if (!pollIsCurrent(generation)) return;
      setVaultActionApprovals({ state: "ready", data, error: null });
      const pending = data.filter((item) => item.status === "approval_pending");
      setVaultActionDialog((current) => reconcileVaultApprovalDialog(current, pending, seenPendingVaultApprovalsRef.current));
    } catch (error) {
      if (!pollIsCurrent(generation)) return;
      setVaultActionApprovals({ state: "error", data: [], error: error.message });
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

  async function loadFileTransferBatches(options = {}, generation) {
    return loadCurrentFileTransferBatches({
      request: () => apiGet("/api/file-transfer-batches?limit=30"),
      pollGeneration: generation,
      pollIsCurrent,
      listState: fileTransferListState,
      onItems: (items) => {
        const pendingApprovals = items.filter((item) => item.status === "pending_approval");
        const hasNewPendingApproval = pendingApprovals.some((item) => !seenPendingTransferApprovalsRef.current.has(item.id));
        pendingApprovals.forEach((item) => seenPendingTransferApprovalsRef.current.add(item.id));
        if (hasNewPendingApproval) setTransferCenterOpen(true);
        setFileTransferBatches({ state: "ready", data: items, error: null });
      },
      onError: (error) => {
        setFileTransferBatches((current) => ({ state: "error", data: options.keepData ? current.data : [], error: error.message }));
      },
    });
  }

  async function refreshAll(generation) {
    await Promise.all([
      loadStatus(generation),
      loadDatabaseStatus(generation),
      loadMCPRuntime(generation),
      loadTargets(generation),
      loadCredentials(generation),
      loadTokens(generation),
      loadConsoleSessions(generation),
      loadConnectorActionApprovals(generation),
      loadVaultActionApprovals(generation),
      loadMessages(generation),
      loadFileTransferBatches({ keepData: true }, generation),
    ]);
  }

  const refreshCurrentRoute = useEffectEvent(async (pathname, firstLoad, generation) => {
    if (firstLoad || pathname !== "/console") {
      await refreshAll(generation);
      return;
    }
    await Promise.all([
      loadStatus(generation),
      loadDatabaseStatus(generation),
      loadTargets(generation),
      loadConsoleSessions(generation),
      loadConnectorActionApprovals(generation),
      loadVaultActionApprovals(generation),
      loadMessages(generation),
      loadFileTransferBatches({ keepData: true }, generation),
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
    const unlocked = databaseStatus.data?.unlocked === true || databaseStatus.data?.state === "unlocked";
    if (databaseStatus.state !== "ready" || !unlocked) {
      document.title = "AIPermission";
      return;
    }
    const runtimeLabel = mcpRuntime.data?.enabled ? "Started" : "Stopped";
    const databaseName = databaseStatus.data?.database_name || databaseStatus.data?.database_id || "Database";
    document.title = `${runtimeLabel} - ${databaseName}`;
  }, [
    databaseStatus.state,
    databaseStatus.data?.unlocked,
    databaseStatus.data?.state,
    databaseStatus.data?.database_name,
    databaseStatus.data?.database_id,
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

  function upsertConsoleSession(session) {
    setConsoleSessions((current) => {
      const index = current.data.findIndex((item) => Number(item.id) === Number(session.id));
      const data = [...current.data];
      if (index === -1) {
        data.unshift(session);
      } else {
        data[index] = { ...data[index], ...session };
      }
      return { state: "ready", data, error: null };
    });
  }

  async function ensureConsoleSession(server) {
    const current = latestSessionForRuntime(consoleSessions.data, server.id);
    if (current) {
      if (isLiveConsoleSession(current)) attachConsoleSession(current.id);
      return current;
    }
    return newConsoleSession(server);
  }

  async function newConsoleSession(server, options = {}) {
    if (options.vaultItems === undefined) {
      try {
        const vaultOptions = await apiGet(`/api/vault-session-options?runtime_id=${encodeURIComponent(server.id)}`);
        if (vaultOptions.supported && ((vaultOptions.items || []).length > 0 || (vaultOptions.defaults || []).length > 0)) {
          vaultSessionResolverRef.current?.resolve(null);
          return new Promise((resolve, reject) => {
            vaultSessionResolverRef.current = { resolve, reject };
            setVaultSessionDialog({
              open: true,
              status: "idle",
              runtime: server,
              options: vaultOptions,
              sessionOptions: options,
              error: null,
            });
          });
        }
      } catch {
        // Vault selection is optional for local sessions; a failed probe must not block a normal console.
      }
    }
    return createConsoleSession(server, options);
  }

  async function createConsoleSession(server, options = {}) {
    const session = await apiPost("/api/console/sessions", {
      runtime_id: server.id,
      name: options.name || `${server.name} shell`,
      close_existing: options.closeExisting !== false,
      params: options.params || undefined,
      vault_items: options.vaultItems || undefined,
    });
    if (options.deferActivation) return session;
    activateConsoleSession(session);
    return session;
  }

  function activateConsoleSession(session) {
    upsertConsoleSession(session);
    window.setTimeout(() => attachConsoleSession(session.id), 0);
  }

  async function startVaultConsoleSession(vaultItems) {
    const current = vaultSessionDialog;
    setVaultSessionDialog((value) => ({ ...value, status: "starting", error: null }));
    try {
      const session = await createConsoleSession(current.runtime, {
        ...current.sessionOptions,
        vaultItems,
        deferActivation: true,
      });
      setVaultSessionDialog({ open: false, status: "idle", runtime: null, options: null, sessionOptions: null, error: null });
      window.setTimeout(() => activateConsoleSession(session), 0);
      vaultSessionResolverRef.current?.resolve(session);
      vaultSessionResolverRef.current = null;
    } catch (error) {
      setVaultSessionDialog((value) => ({ ...value, status: "error", error: error.message }));
    }
  }

  function closeVaultSessionDialog() {
    vaultSessionResolverRef.current?.resolve(null);
    vaultSessionResolverRef.current = null;
    setVaultSessionDialog({ open: false, status: "idle", runtime: null, options: null, sessionOptions: null, error: null });
  }

  function cancelConsoleCommand(sessionID) {
    sendConsoleInput(sessionID, "\u0003");
  }

  async function restartConsoleRuntime(runtimeID) {
    const affectedSessions = consoleSessions.data.filter((session) => Number(session.runtime_id) === Number(runtimeID));
    disconnectConsoleSessions(affectedSessions.map((session) => session.id));
    const result = await apiPost(`/api/console/runtime-surfaces/${runtimeID}/restart`, {});
    await loadConsoleSessions();
    return result;
  }

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

  async function runVaultActionApproval() {
    const approval = vaultActionDialog.approval;
    if (!approval) return;
    setVaultActionDialog((current) => ({ ...current, state: "running", error: null }));
    try {
      await apiPost(`/api/vault-action-approvals/${approval.id}/run`, { user_note: vaultActionDialog.note });
      setVaultActionDialog({ approval: null, note: "", state: "idle", error: null });
      await Promise.all([loadVaultActionApprovals(), loadConsoleSessions()]);
    } catch (error) {
      await loadVaultActionApprovals();
      setVaultActionDialog((current) => ({
        ...current,
        state: error.message.toLowerCase().includes("stale") || error.message.toLowerCase().includes("changed") ? "stale" : "failed",
        error: error.message,
      }));
    }
  }

  async function declineVaultActionApproval() {
    const approval = vaultActionDialog.approval;
    if (!approval) return;
    setVaultActionDialog((current) => ({ ...current, state: "declining", error: null }));
    try {
      await apiPost(`/api/vault-action-approvals/${approval.id}/decline`, { user_note: vaultActionDialog.note });
      setVaultActionDialog({ approval: null, note: "", state: "idle", error: null });
      await loadVaultActionApprovals();
    } catch (error) {
      setVaultActionDialog((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function closeVaultActionApproval() {
    setVaultActionDialog({ approval: null, note: "", state: "idle", error: null });
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

  function applyFileTransferBatch(batch) {
    setFileTransferBatches((current) => fileTransferListState.applyBatch(current, batch));
  }

  const transferBatchActions = createFileTransferBatchActions({
    post: apiPost,
    applyResult: applyFileTransferBatch,
    refresh: loadFileTransferBatches,
  });

  function requestLockDatabase() {
    const unlockedCount = (databaseStatus.data?.databases || []).filter((item) => item.unlocked).length;
    if (unlockedCount > 1) {
      setLockDialog({ open: true, state: "idle", error: null });
      return;
    }
    void lockDatabase("current");
  }

  async function lockDatabase(scope) {
    setLockDialog((current) => ({ ...current, state: "locking", error: null }));
    disconnectAllConsoleSessions();
    try {
      await apiPost("/api/lock", { scope });
      window.location.reload();
    } catch (error) {
      setLockDialog((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function openSwitchDialog() {
    setSwitchDialog({
      open: true,
      database_id: databaseStatus.data?.database_id || databaseStatus.data?.databases?.[0]?.id || "",
      password: "",
      state: "idle",
      error: null,
    });
  }

  async function switchDatabase(event) {
    event?.preventDefault();
    const currentID = databaseStatus.data?.database_id;
    if (switchDialog.database_id === currentID) {
      setSwitchDialog((current) => ({ ...current, open: false }));
      return;
    }
    setSwitchDialog((current) => ({ ...current, state: "switching", error: null }));
    try {
      disconnectAllConsoleSessions();
      await apiPost("/api/databases/switch", {
        database_id: switchDialog.database_id,
        password: switchDialog.password,
      });
      window.location.reload();
    } catch (error) {
      setSwitchDialog((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  const pendingConnectorActionApprovalCount = connectorActionApprovals.data.filter(
    (approval) => approval.status === "approval_pending",
  ).length;
  const pendingVaultActionApprovalCount = vaultActionApprovals.data.filter((approval) => approval.status === "approval_pending").length;
  const unreadMessageCount = messages.data.filter(isUnreadMessage).length;
  const consoleAttentionCount = pendingConnectorActionApprovalCount + pendingVaultActionApprovalCount + unreadMessageCount;
  const activeTransferCount = fileTransferBatches.data.filter(isActiveTransferBatch).length;

  return (
    <main className="min-h-screen bg-stone-100 text-stone-950">
      <AppSidebar
        pathname={location.pathname}
        consoleAttentionCount={consoleAttentionCount}
        activeTransferCount={activeTransferCount}
        gatewayState={gatewayState}
        mcpRuntime={mcpRuntime}
        theme={theme}
        onSetTheme={setTheme}
        onSetMCPRuntimeEnabled={setMCPRuntimeEnabled}
        onOpenTransferCenter={() => setTransferCenterOpen(true)}
        onSwitchDatabase={openSwitchDialog}
        onLockDatabase={requestLockDatabase}
      />

      <TransferCenter
        open={transferCenterOpen}
        batches={fileTransferBatches.data}
        state={fileTransferBatches.state}
        error={fileTransferBatches.error}
        onClose={() => setTransferCenterOpen(false)}
        onRefresh={() => loadFileTransferBatches({ keepData: true })}
        onPause={transferBatchActions.pause}
        onResume={transferBatchActions.resume}
        onCancel={transferBatchActions.cancel}
        onApprove={transferBatchActions.approve}
        onDecline={transferBatchActions.decline}
      />
      <VaultSessionDialog state={vaultSessionDialog} onClose={closeVaultSessionDialog} onStart={startVaultConsoleSession} />
      <VaultActionApprovalDialog
        approval={vaultActionDialog.approval}
        note={vaultActionDialog.note}
        action={vaultActionDialog}
        onNoteChange={(note) => setVaultActionDialog((current) => ({ ...current, note }))}
        onRun={runVaultActionApproval}
        onDecline={declineVaultActionApproval}
        onClose={closeVaultActionApproval}
      />

      <DatabaseSwitchDialog
        state={switchDialog}
        databaseStatus={databaseStatus.data}
        onChange={setSwitchDialog}
        onClose={() => setSwitchDialog((current) => ({ ...current, open: false }))}
        onSubmit={switchDatabase}
      />

      <Dialog
        open={lockDialog.open}
        title="Lock database"
        description="More than one database is currently unlocked. Choose what should be locked."
        onClose={() => setLockDialog({ open: false, state: "idle", error: null })}
        size="md"
      >
        <div className="grid gap-4">
          <Notice>
            Lock current closes only the active database and switches to another unlocked database if one is available. Lock all closes
            every unlocked database and stops MCP access until a database is unlocked again.
          </Notice>
          {lockDialog.error ? <Notice tone="bad">{lockDialog.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" disabled={lockDialog.state === "locking"} onClick={() => lockDatabase("current")}>
              Lock current
            </Button>
            <Button type="button" variant="danger" disabled={lockDialog.state === "locking"} onClick={() => lockDatabase("all")}>
              Lock all
            </Button>
          </div>
        </div>
      </Dialog>

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
              consoleSessions,
              loadConsoleSessions,
              ensureConsoleSession,
              newConsoleSession,
              attachConsoleSession,
              closeConsoleSession,
              cancelConsoleCommand,
              restartConsoleRuntime,
              sendConsoleInput,
              resizeConsoleSession,
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
