import { useEffect, useMemo, useRef, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { apiGet, apiPost, apiPut, apiUrl } from "../lib/api";
import { AppSidebar } from "./app-sidebar";
import { DatabaseSwitchDialog } from "./database-switch-dialog";
import { TransferCenter } from "./transfer-center";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Notice } from "./ui/notice";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { getConnectorModel } from "../connectors/templates/registry";
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
  const [messages, setMessages] = useState({ state: "loading", data: [], error: null });
  const [fileTransferBatches, setFileTransferBatches] = useState({ state: "loading", data: [], error: null });
  const [databaseStatus, setDatabaseStatus] = useState({ state: "loading", data: null, error: null });
  const [mcpRuntime, setMCPRuntime] = useState({ state: "loading", data: { enabled: false, start_enabled: false }, error: null });
  const [switchDialog, setSwitchDialog] = useState({ open: false, database_id: "", password: "", state: "idle", error: null });
  const [lockDialog, setLockDialog] = useState({ open: false, state: "idle", error: null });
  const [transferCenterOpen, setTransferCenterOpen] = useState(false);
  const consoleConnectionsRef = useRef({});
  const seenPendingTransferApprovalsRef = useRef(new Set());

  async function loadStatus() {
    try {
      const data = await apiGet("/api/status");
      setStatus({ state: "ready", data, error: null });
    } catch (error) {
      setStatus({ state: "error", data: null, error: error.message });
    }
  }

  async function loadDatabaseStatus() {
    try {
      const data = await apiGet("/api/unlock/status");
      setDatabaseStatus({ state: "ready", data, error: null });
    } catch (error) {
      setDatabaseStatus({ state: "error", data: null, error: error.message });
    }
  }

  async function loadTargets() {
    try {
      const data = await apiGet("/api/targets");
      setTargets({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  async function loadCredentials() {
    try {
      const results = await Promise.allSettled(
        supportedConnectorKinds.map(async (kind) => {
          const model = getConnectorModel(kind);
          if (!model?.loadCredentialResources) return [];
          const items = await model.loadCredentialResources();
          return normalizeCredentialResources(kind, items);
        })
      );
      const data = results.flatMap((result) => (result.status === "fulfilled" ? result.value : []));
      const errors = results
        .map((result, index) => (result.status === "rejected" ? `${supportedConnectorKinds[index]}: ${result.reason?.message || result.reason}` : ""))
        .filter(Boolean);
      setCredentials({ state: "ready", data, error: null, errors });
    } catch (error) {
      setCredentials({ state: "error", data: [], error: error.message, errors: [] });
    }
  }

  async function loadTokens() {
    try {
      const data = await apiGet("/api/tokens");
      setTokens({ state: "ready", data, error: null });
      return data;
    } catch (error) {
      setTokens({ state: "error", data: [], error: error.message });
      return [];
    }
  }

  async function loadConsoleSessions() {
    try {
      const data = await apiGet("/api/console/sessions");
      setConsoleSessions((current) => ({ state: "ready", data: mergeConsoleSessionData(data, current.data), error: null }));
      data
        .filter((session) => isLiveConsoleSession(session))
        .forEach((session) => attachConsoleSession(session.id));
    } catch (error) {
      setConsoleSessions({ state: "error", data: [], error: error.message });
    }
  }

  async function loadConnectorActionApprovals() {
    try {
      const data = await apiGet("/api/connector-action-approvals");
      setConnectorActionApprovals({ state: "ready", data, error: null });
    } catch (error) {
      setConnectorActionApprovals({ state: "error", data: [], error: error.message });
    }
  }

  async function loadMessages() {
    try {
      const data = await apiGet("/api/messages");
      setMessages({ state: "ready", data, error: null });
    } catch (error) {
      setMessages({ state: "error", data: [], error: error.message });
    }
  }

  async function loadMCPRuntime() {
    try {
      const data = await apiGet("/api/settings/mcp-runtime");
      setMCPRuntime({ state: "ready", data, error: null });
      return data;
    } catch (error) {
      setMCPRuntime({ state: "error", data: { enabled: false, start_enabled: false }, error: error.message });
      return { enabled: false, start_enabled: false };
    }
  }

  async function loadFileTransferBatches(options = {}) {
    try {
      const data = await apiGet("/api/file-transfer-batches?limit=30");
      const items = data.items || [];
      const pendingApprovals = items.filter((item) => item.status === "pending_approval");
      const hasNewPendingApproval = pendingApprovals.some((item) => !seenPendingTransferApprovalsRef.current.has(item.id));
      pendingApprovals.forEach((item) => seenPendingTransferApprovalsRef.current.add(item.id));
      if (hasNewPendingApproval) {
        setTransferCenterOpen(true);
      }
      setFileTransferBatches({ state: "ready", data: items, error: null });
      return items;
    } catch (error) {
      setFileTransferBatches((current) => ({ state: "error", data: options.keepData ? current.data : [], error: error.message }));
      return [];
    }
  }

  async function refreshAll() {
    await Promise.all([loadStatus(), loadDatabaseStatus(), loadMCPRuntime(), loadTargets(), loadCredentials(), loadTokens(), loadConsoleSessions(), loadConnectorActionApprovals(), loadMessages(), loadFileTransferBatches({ keepData: true })]);
  }

  useEffect(() => {
    let cancelled = false;
    let firstLoad = true;
    async function load() {
      if (cancelled) return;
      if (firstLoad) {
        firstLoad = false;
        await refreshAll();
        return;
      }
      if (location.pathname === "/console") {
        await Promise.all([loadStatus(), loadDatabaseStatus(), loadTargets(), loadConsoleSessions(), loadConnectorActionApprovals(), loadMessages(), loadFileTransferBatches({ keepData: true })]);
      } else {
        await refreshAll();
      }
    }
    load();
    const timer = setInterval(load, 5000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [location.pathname]);

  useEffect(() => {
    return () => {
      Object.values(consoleConnectionsRef.current).forEach((connection) => connection?.close());
      consoleConnectionsRef.current = {};
    };
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
  }, [databaseStatus.state, databaseStatus.data?.unlocked, databaseStatus.data?.state, databaseStatus.data?.database_name, databaseStatus.data?.database_id, mcpRuntime.data?.enabled]);

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
    return { state: "ready", data: liveConsoleRuntimeTargets(targets.data), error: null };
  }, [targets.state, targets.data, targets.error]);

  function patchConsoleSession(sessionID, updater) {
    setConsoleSessions((current) => {
      const index = current.data.findIndex((session) => Number(session.id) === Number(sessionID));
      if (index === -1) return current;
      const data = [...current.data];
      data[index] = {
        ...data[index],
        ...updater(data[index]),
      };
      return {
        state: "ready",
        data,
        error: null,
      };
    });
  }

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
    const session = await apiPost("/api/console/sessions", {
      runtime_id: server.id,
      name: options.name || `${server.name} shell`,
      close_existing: true,
      params: options.params || undefined,
    });
    upsertConsoleSession(session);
    attachConsoleSession(session.id);
    return session;
  }

  function attachConsoleSession(sessionID, options = {}) {
    const existing = consoleConnectionsRef.current[sessionID];
    if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
      if (!options.force) return;
      existing.close();
    }
    if (existing && (existing.readyState === WebSocket.CLOSING || existing.readyState === WebSocket.CLOSED)) {
      delete consoleConnectionsRef.current[sessionID];
    }

    patchConsoleSession(sessionID, () => ({ status: "connecting", error: null }));
    const socket = new WebSocket(consoleSessionAttachUrl(sessionID));
    consoleConnectionsRef.current[sessionID] = socket;

    socket.onopen = () => {
      if (consoleConnectionsRef.current[sessionID] !== socket) return;
    };
    socket.onmessage = (event) => {
      if (consoleConnectionsRef.current[sessionID] !== socket) return;
      const message = JSON.parse(event.data);
      if (message.type === "snapshot") {
        patchConsoleSession(sessionID, () => ({
          transcript: message.data || "",
          status: message.status || "connected",
          error: null,
        }));
      }
      if (message.type === "ready") {
        patchConsoleSession(sessionID, () => ({ status: "connected", error: null }));
      }
      if (message.type === "output") {
        patchConsoleSession(sessionID, (session) => ({
          transcript: limitTranscript(`${session.transcript || ""}${message.data || ""}`),
          status: message.status || "connected",
          error: null,
        }));
      }
      if (message.type === "error") {
        patchConsoleSession(sessionID, (session) => ({
          transcript: limitTranscript(`${session.transcript || ""}\r\n${message.data || "PTY error"}\r\n`),
          status: "error",
          error: message.data || "PTY error",
        }));
      }
      if (message.type === "exit") {
        patchConsoleSession(sessionID, (session) => ({
          transcript: limitTranscript(`${session.transcript || ""}\r\n[session closed]\r\n`),
          status: message.status || "closed",
          error: message.data || "",
        }));
      }
    };
    socket.onerror = () => {
      if (consoleConnectionsRef.current[sessionID] !== socket) return;
      patchConsoleSession(sessionID, () => ({ status: "error", error: "PTY connection failed." }));
    };
    socket.onclose = () => {
      if (consoleConnectionsRef.current[sessionID] !== socket) return;
      delete consoleConnectionsRef.current[sessionID];
    };
  }

  function cancelConsoleCommand(sessionID) {
    sendConsoleInput(sessionID, "\u0003");
  }

  function sendConsoleInput(sessionID, data) {
    const socket = consoleConnectionsRef.current[sessionID];
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "input", data }));
      return;
    }
    attachConsoleSession(sessionID);
    void apiPost(`/api/console/sessions/${sessionID}/input`, { data }).catch((error) => {
      patchConsoleSession(sessionID, () => ({ status: "error", error: error.message }));
    });
  }

  function resizeConsoleSession(sessionID, cols, rows) {
    const socket = consoleConnectionsRef.current[sessionID];
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", cols, rows }));
    }
  }

  async function closeConsoleSession(sessionID) {
    await apiPost(`/api/console/sessions/${sessionID}/close`, {});
    patchConsoleSession(sessionID, () => ({ status: "closed" }));
  }

  async function restartConsoleRuntime(runtimeID) {
    const affectedSessions = consoleSessions.data.filter((session) => Number(session.runtime_id) === Number(runtimeID));
    affectedSessions.forEach((session) => {
      const connection = consoleConnectionsRef.current[session.id];
      if (connection) {
        connection.close();
        delete consoleConnectionsRef.current[session.id];
      }
    });
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

  async function pauseFileTransferBatch(batchID) {
    await apiPost(`/api/file-transfer-batches/${batchID}/pause`, {});
    await loadFileTransferBatches({ keepData: true });
  }

  async function resumeFileTransferBatch(batchID) {
    await apiPost(`/api/file-transfer-batches/${batchID}/resume`, {});
    await loadFileTransferBatches({ keepData: true });
  }

  async function cancelFileTransferBatch(batchID) {
    await apiPost(`/api/file-transfer-batches/${batchID}/cancel`, {});
    await loadFileTransferBatches({ keepData: true });
  }

  async function approveFileTransferBatch(batchID, itemIDs, note = "") {
    await apiPost(`/api/file-transfer-batches/${batchID}/approve`, { item_ids: itemIDs, note });
    await loadFileTransferBatches({ keepData: true });
  }

  async function declineFileTransferBatch(batchID, note = "") {
    await apiPost(`/api/file-transfer-batches/${batchID}/decline`, { note });
    await loadFileTransferBatches({ keepData: true });
  }

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
    Object.values(consoleConnectionsRef.current).forEach((connection) => connection?.close());
    consoleConnectionsRef.current = {};
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
      Object.values(consoleConnectionsRef.current).forEach((connection) => connection?.close());
      consoleConnectionsRef.current = {};
      await apiPost("/api/databases/switch", {
        database_id: switchDialog.database_id,
        password: switchDialog.password,
      });
      window.location.reload();
    } catch (error) {
      setSwitchDialog((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  const pendingConnectorActionApprovalCount = connectorActionApprovals.data.filter((approval) => approval.status === "approval_pending").length;
  const unreadMessageCount = messages.data.filter(isUnreadMessage).length;
  const consoleAttentionCount = pendingConnectorActionApprovalCount + unreadMessageCount;
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
        onPause={pauseFileTransferBatch}
        onResume={resumeFileTransferBatch}
        onCancel={cancelFileTransferBatch}
        onApprove={approveFileTransferBatch}
        onDecline={declineFileTransferBatch}
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
            Lock current closes only the active database and switches to another unlocked database if one is available. Lock all closes every unlocked database and stops MCP access until a database is unlocked again.
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

function normalizeCredentialResources(connectorKind, items) {
  return (items || []).map((item) => {
    const resourceKind = item.resource_kind || item.kind || "credential";
    return {
      ...item,
      connector_kind: item.connector_kind || connectorKind,
      resource_kind: resourceKind,
      resource_ref: item.resource_ref || `${connectorKind}:${resourceKind}:${item.id || item.name || "unknown"}`,
    };
  });
}

function isUnreadMessage(message) {
  return message.direction === "ai_to_user" && !message.consumed_at;
}

function isActiveTransferBatch(batch) {
  return batch?.status === "pending_approval" || batch?.status === "pending" || batch?.status === "running" || batch?.status === "paused";
}

function consoleSessionAttachUrl(sessionID) {
  const url = new URL(apiUrl, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = `/api/console/sessions/${sessionID}/attach`;
  return url.toString();
}

function limitTranscript(value) {
  const maxLength = 200000;
  if (value.length <= maxLength) return value;
  return value.slice(value.length - maxLength);
}

function isLiveConsoleSession(session) {
  return session?.status === "connecting" || session?.status === "connected";
}

function latestSessionForRuntime(sessions, runtimeID) {
  return sessions.find((session) => Number(session.runtime_id) === Number(runtimeID)) || null;
}

function liveConsoleRuntimeTargets(targets) {
  return (targets || [])
    .filter((target) => {
      const model = getConnectorModel(target.connector_kind);
      return Boolean(model?.usesLiveConsole?.({ target }) && target.runtime_id && model?.liveConsoleRuntimeTarget);
    })
    .map((target) => {
      const model = getConnectorModel(target.connector_kind);
      return model.liveConsoleRuntimeTarget({ target });
    });
}

function mergeConsoleSessionData(next, current) {
  return next.map((session) => {
    const local = current.find((item) => Number(item.id) === Number(session.id));
    if (!local) return session;
    if (isLiveConsoleSession(local) && (session.status === "connecting" || session.status === "connected")) {
      return { ...session, transcript: local.transcript, status: local.status, error: local.error };
    }
    return session;
  });
}
