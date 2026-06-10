import { AlertTriangle, Circle, Clock, Database, Files, ListChecks, MessageSquare, PanelLeftClose, PanelLeftOpen, RefreshCcw, Server, Square, TerminalSquare, XCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { apiGet, apiPost } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { effectiveRule, permissionLifetimeLabel } from "../lib/permissions";
import { useTokenPermissions } from "../lib/use-token-permissions";
import { Badge, CountBadge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Notice } from "../components/ui/notice";
import { ApprovalDialog } from "../components/console/approval-dialog";
import { BulkCommandDialog } from "../components/console/bulk-command-dialog";
import { ConnectorActionApprovalDialog } from "../components/console/connector-action-approval-dialog";
import { ConnectorActivityDialog } from "../components/console/connector-activity-dialog";
import { FileTransferDialog } from "../components/console/file-transfer-dialog";
import { MessagesDialog } from "../components/console/messages-dialog";
import { NoLiveSession } from "../components/console/no-live-session";
import { PtyConsole } from "../components/console/pty-console";
import { TokenPermissionPanel } from "../components/console/token-permission-panel";
import { emptySession, isUnreadMessage, latestSessionForServer } from "../components/console/helpers";
import { useConsolePageState } from "../components/console/use-console-page-state";

export function ConsolePage() {
  const {
    servers,
    targets,
    tokens,
    approvals,
    connectorActionApprovals,
    messages,
    loadTokens,
    loadApprovals,
    loadConnectorActionApprovals,
    loadMessages,
    markMessagesRead,
    consoleSessions,
    newConsoleSession,
    attachConsoleSession,
    closeConsoleSession,
    cancelConsoleCommand,
    restartConsoleSession,
    sendConsoleInput,
    resizeConsoleSession,
    runApproval,
    declineApproval,
    runConnectorActionApproval,
    declineConnectorActionApproval,
    mcpRuntime,
    theme,
  } = useGateway();
  const [searchParams, setSearchParams] = useSearchParams();
  const { permissionState, loadAllTokenPermissions, setTokenServerRule } = useTokenPermissions(tokens.data);
  const [activeApprovalID, setActiveApprovalID] = useState(null);
  const [activeApprovalSnapshot, setActiveApprovalSnapshot] = useState(null);
  const [dismissedApprovalIDs, setDismissedApprovalIDs] = useState({});
  const [approvalNote, setApprovalNote] = useState("");
  const [approvalAction, setApprovalAction] = useState({ state: "idle", error: null });
  const [activeConnectorApprovalID, setActiveConnectorApprovalID] = useState(null);
  const [activeConnectorApprovalSnapshot, setActiveConnectorApprovalSnapshot] = useState(null);
  const [dismissedConnectorApprovalIDs, setDismissedConnectorApprovalIDs] = useState({});
  const [connectorApprovalNote, setConnectorApprovalNote] = useState("");
  const [connectorApprovalAction, setConnectorApprovalAction] = useState({ state: "idle", error: null });
  const [messagesOpen, setMessagesOpen] = useState(false);
  const [messagesState, setMessagesState] = useState({ state: "idle", data: [], error: null });
  const [messageText, setMessageText] = useState("");
  const [messageTokenID, setMessageTokenID] = useState("");
  const [serversCompact, setServersCompact] = useState(false);
  const [tokensCompact, setTokensCompact] = useState(false);
  const [fileTransferOpen, setFileTransferOpen] = useState(false);
  const [bulkCommandOpen, setBulkCommandOpen] = useState(false);
  const [connectorActivityOpen, setConnectorActivityOpen] = useState(false);
  const [restartAction, setRestartAction] = useState({ state: "idle", error: null });
  const [now, setNow] = useState(Date.now());

  const selectedTargetRef = searchParams.get("target");
  const legacySelectedServerID = searchParams.get("server");
  const sessions = consoleSessions.data || [];
  const targetItems = targets?.data || [];
  const rawPendingApprovals = approvals.data.filter((approval) => approval.status === "pending_approval");
  const rawUnreadMessages = messages.data.filter(isUnreadMessage);
  const pendingConnectorApprovals = (connectorActionApprovals?.data || []).filter((approval) => approval.status === "approval_pending");
  const connectorActionCount = connectorActionApprovals?.data?.length || 0;
  const defaultTargetRef = useMemo(
    () => defaultConsoleTargetRef(targetItems, rawPendingApprovals, rawUnreadMessages, pendingConnectorApprovals),
    [
      targetItems.map((target) => `${target.ref}:${target.server_id || ""}`).join(","),
      rawPendingApprovals.map((approval) => `${approval.id}:${approval.server_id}`).join(","),
      rawUnreadMessages.map((message) => `${message.id}:${message.server_id}`).join(","),
      pendingConnectorApprovals.map((approval) => `${approval.id}:${approval.target_ref}`).join(","),
    ]
  );
  const selectedTarget = useMemo(() => {
    if (!targetItems.length) return null;
    if (selectedTargetRef) {
      const exact = targetItems.find((target) => target.ref === selectedTargetRef);
      if (exact) return exact;
    }
    if (legacySelectedServerID) {
      const legacySSH = targetItems.find((target) => target.connector_kind === "ssh" && String(target.server_id) === legacySelectedServerID);
      if (legacySSH) return legacySSH;
    }
    return targetItems.find((target) => target.ref === defaultTargetRef) || targetItems[0];
  }, [targetItems, selectedTargetRef, legacySelectedServerID, defaultTargetRef]);
  const selectedServerID = selectedTarget?.connector_kind === "ssh" ? String(selectedTarget.server_id || "") : "";
  const {
    selectedServer,
    selectedSession,
    selectedSessionLive,
    pendingApprovals,
    unreadMessages,
    selectedPendingApprovals,
    selectedUnreadMessages,
    selectedTokenOptions,
  } = useConsolePageState({
    servers,
    tokens,
    approvals,
    messages,
    sessions,
    selectedServerID,
    permissionState,
    now,
    allowServerFallback: false,
  });
  const activePendingApproval = activeApprovalID ? pendingApprovals.find((approval) => Number(approval.id) === Number(activeApprovalID)) : null;
  const activeApproval = activePendingApproval || (activeApprovalSnapshot && Number(activeApprovalSnapshot.id) === Number(activeApprovalID) ? activeApprovalSnapshot : null);
  const activePendingConnectorApproval = activeConnectorApprovalID ? pendingConnectorApprovals.find((approval) => Number(approval.id) === Number(activeConnectorApprovalID)) : null;
  const activeConnectorApproval = activePendingConnectorApproval || (activeConnectorApprovalSnapshot && Number(activeConnectorApprovalSnapshot.id) === Number(activeConnectorApprovalID) ? activeConnectorApprovalSnapshot : null);
  const alwaysRunTokens = selectedServer
    ? selectedTokenOptions.filter((token) => effectiveRule(permissionState.data?.[token.id]?.[selectedServer.id], now) === "always_run")
    : [];
  const temporaryAlwaysRunLabels = selectedServer
    ? alwaysRunTokens
        .map((token) => permissionState.data?.[token.id]?.[selectedServer.id])
        .filter((permission) => permission?.expires_at)
        .map((permission) => permissionLifetimeLabel(permission, now))
    : [];
  const showAlwaysRunWarning = Boolean(mcpRuntime?.data?.enabled && selectedServer && alwaysRunTokens.length > 0);
  const selectedRunningRequests = selectedServer
    ? approvals.data.filter((approval) => approval.status === "running" && Number(approval.server_id) === Number(selectedServer.id))
    : [];
  const selectedRunningRequest = selectedRunningRequests[0] || null;
  const consoleBannerCount = (showAlwaysRunWarning ? 1 : 0) + (selectedRunningRequest ? 1 : 0);

  useEffect(() => {
    if (targetItems.length === 0 || !defaultTargetRef) return;
    if (!selectedTargetRef || !targetItems.some((target) => target.ref === selectedTargetRef)) {
      setSearchParams({ target: selectedTarget?.ref || defaultTargetRef }, { replace: true });
    }
  }, [targetItems, selectedTargetRef, selectedTarget?.ref, defaultTargetRef, setSearchParams]);

  useEffect(() => {
    if (tokens.state !== "ready") return;
    loadAllTokenPermissions(tokens.data);
  }, [tokens.state, tokens.data.map((token) => token.id).join(",")]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!selectedServer) return;
    if (selectedSessionLive) {
      attachConsoleSession(selectedSession.id);
    }
  }, [selectedServer?.id, selectedSession.id, selectedSession.status]);

  useEffect(() => {
    setRestartAction({ state: "idle", error: null });
  }, [selectedServer?.id, selectedRunningRequest?.id]);

  useEffect(() => {
    if (activeApprovalID && !pendingApprovals.some((approval) => Number(approval.id) === Number(activeApprovalID)) && !["error", "failed", "running", "stale"].includes(approvalAction.state)) {
      setActiveApprovalID(null);
      setActiveApprovalSnapshot(null);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
      return;
    }
    if (activeApprovalID || selectedPendingApprovals.length === 0) return;
    const next = selectedPendingApprovals.find((approval) => !dismissedApprovalIDs[approval.id]);
    if (next) {
      setActiveApprovalID(next.id);
      setActiveApprovalSnapshot(next);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
    }
  }, [activeApprovalID, selectedPendingApprovals.map((approval) => approval.id).join(","), dismissedApprovalIDs, pendingApprovals.length, approvalAction.state]);

  useEffect(() => {
    if (activeConnectorApprovalID && !pendingConnectorApprovals.some((approval) => Number(approval.id) === Number(activeConnectorApprovalID)) && !["error", "failed", "running", "stale"].includes(connectorApprovalAction.state)) {
      setActiveConnectorApprovalID(null);
      setActiveConnectorApprovalSnapshot(null);
      setConnectorApprovalNote("");
      setConnectorApprovalAction({ state: "idle", error: null });
      return;
    }
    if (activeConnectorApprovalID || pendingConnectorApprovals.length === 0) return;
    const next = pendingConnectorApprovals.find((approval) => !dismissedConnectorApprovalIDs[approval.id]);
    if (next) {
      setActiveConnectorApprovalID(next.id);
      setActiveConnectorApprovalSnapshot(next);
      setConnectorApprovalNote("");
      setConnectorApprovalAction({ state: "idle", error: null });
    }
  }, [activeConnectorApprovalID, pendingConnectorApprovals.map((approval) => approval.id).join(","), dismissedConnectorApprovalIDs, pendingConnectorApprovals.length, connectorApprovalAction.state]);

  function selectTarget(targetRef) {
    setSearchParams({ target: targetRef });
  }

  function openApproval(approval) {
    setActiveApprovalID(approval.id);
    setActiveApprovalSnapshot(approval);
    setApprovalNote(approval.user_note || "");
    setApprovalAction({ state: "idle", error: null });
  }

  function openConnectorApproval(approval) {
    setActiveConnectorApprovalID(approval.id);
    setActiveConnectorApprovalSnapshot(approval);
    setConnectorApprovalNote("");
    setConnectorApprovalAction({ state: "idle", error: null });
  }

  async function loadServerMessages() {
    if (!selectedServer) return;
    setMessagesState((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet(`/api/messages?server_id=${selectedServer.id}`);
      setMessagesState({ state: "ready", data, error: null });
    } catch (error) {
      setMessagesState({ state: "error", data: [], error: error.message });
    }
  }

  function openMessages(preferredTokenID = "") {
    const unreadToken = selectedUnreadMessages[0]?.token_id;
    const firstToken = selectedTokenOptions[0];
    const nextTokenID = preferredTokenID || unreadToken || messageTokenID || (firstToken ? String(firstToken.id) : "");
    setMessageTokenID(nextTokenID ? String(nextTokenID) : "");
    setMessagesOpen(true);
    void loadServerMessages();
  }

  function closeMessages() {
    setMessagesOpen(false);
    if (selectedServer && selectedUnreadMessages.length > 0) {
      void markMessagesRead(selectedServer.id);
    }
  }

  async function sendUserMessage(event) {
    event.preventDefault();
    if (!selectedServer || !messageText.trim() || !messageTokenID) return;
    setMessagesState((current) => ({ ...current, state: "sending", error: null }));
    try {
      await apiPost("/api/messages", {
        token_id: Number(messageTokenID),
        server_id: selectedServer.id,
        session_id: selectedSessionLive ? selectedSession.id : null,
        direction: "user_to_ai",
        message: messageText,
      });
      setMessageText("");
      await Promise.all([loadServerMessages(), loadMessages()]);
    } catch (error) {
      setMessagesState((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function closeApprovalDialog() {
    if (activeApprovalID) {
      setDismissedApprovalIDs((current) => ({ ...current, [activeApprovalID]: true }));
    }
    setActiveApprovalID(null);
    setActiveApprovalSnapshot(null);
    setApprovalNote("");
    setApprovalAction({ state: "idle", error: null });
  }

  function closeConnectorApprovalDialog() {
    if (activeConnectorApprovalID) {
      setDismissedConnectorApprovalIDs((current) => ({ ...current, [activeConnectorApprovalID]: true }));
    }
    setActiveConnectorApprovalID(null);
    setActiveConnectorApprovalSnapshot(null);
    setConnectorApprovalNote("");
    setConnectorApprovalAction({ state: "idle", error: null });
  }

  async function approveActiveRequest() {
    if (!activeApproval) return;
    const approval = activeApproval;
    setApprovalAction({ state: "running", error: null });
    try {
      const item = await runApproval(approval.id, approvalNote);
      if (item?.status === "error" || item?.status === "failed") {
        setActiveApprovalSnapshot({ ...approval, ...item });
        setApprovalAction({ state: "failed", error: item.error || "Approval run failed before the command could complete." });
        return;
      }
      setDismissedApprovalIDs((current) => {
        const next = { ...current };
        delete next[approval.id];
        return next;
      });
      setActiveApprovalID(null);
      setActiveApprovalSnapshot(null);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
    } catch (error) {
      setActiveApprovalSnapshot(approval);
      setApprovalAction({ state: isStaleApprovalError(error) ? "stale" : "error", error: error.message });
    }
  }

  async function declineActiveRequest() {
    if (!activeApproval) return;
    setApprovalAction({ state: "declining", error: null });
    try {
      await declineApproval(activeApproval.id, approvalNote);
      setDismissedApprovalIDs((current) => {
        const next = { ...current };
        delete next[activeApproval.id];
        return next;
      });
      setActiveApprovalID(null);
      setActiveApprovalSnapshot(null);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
    } catch (error) {
      setApprovalAction({ state: "error", error: error.message });
    }
  }

  async function approveActiveConnectorRequest() {
    if (!activeConnectorApproval) return;
    const approval = activeConnectorApproval;
    setConnectorApprovalAction({ state: "running", error: null });
    try {
      const item = await runConnectorActionApproval(approval.id);
      if (item?.status === "error" || item?.status === "failed" || item?.status === "stale") {
        setActiveConnectorApprovalSnapshot({ ...approval, ...item });
        setConnectorApprovalAction({ state: item.status === "stale" ? "stale" : "failed", error: item.error || "Connector action failed." });
        return;
      }
      setDismissedConnectorApprovalIDs((current) => {
        const next = { ...current };
        delete next[approval.id];
        return next;
      });
      setActiveConnectorApprovalID(null);
      setActiveConnectorApprovalSnapshot(null);
      setConnectorApprovalNote("");
      setConnectorApprovalAction({ state: "idle", error: null });
    } catch (error) {
      setActiveConnectorApprovalSnapshot(approval);
      setConnectorApprovalAction({ state: isStaleApprovalError(error) ? "stale" : "error", error: error.message });
    }
  }

  async function declineActiveConnectorRequest() {
    if (!activeConnectorApproval) return;
    setConnectorApprovalAction({ state: "declining", error: null });
    try {
      await declineConnectorActionApproval(activeConnectorApproval.id, connectorApprovalNote);
      setDismissedConnectorApprovalIDs((current) => {
        const next = { ...current };
        delete next[activeConnectorApproval.id];
        return next;
      });
      setActiveConnectorApprovalID(null);
      setActiveConnectorApprovalSnapshot(null);
      setConnectorApprovalNote("");
      setConnectorApprovalAction({ state: "idle", error: null });
    } catch (error) {
      setConnectorApprovalAction({ state: "error", error: error.message });
    }
  }

  function isStaleApprovalError(error) {
    const message = String(error?.message || "").toLowerCase();
    return message.includes("stale") || message.includes("approval context") || message.includes("fresh request");
  }

  async function restartSelectedConsoleSession() {
    if (!selectedServer) return;
    setRestartAction({ state: "running", error: null });
    try {
      await restartConsoleSession(selectedServer.id);
      setRestartAction({ state: "idle", error: null });
    } catch (error) {
      setRestartAction({ state: "error", error: error.message });
    }
  }

  return (
    <section
      className="grid h-[calc(100vh-40px)] min-h-[640px] gap-4"
      style={{
        gridTemplateColumns: `${serversCompact ? "56px" : "260px"} minmax(0, 1fr) ${tokensCompact ? "56px" : "360px"}`,
      }}
    >
      <aside className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
        <div className={`border-b border-stone-200 ${serversCompact ? "grid gap-2 p-2" : "flex items-center justify-between gap-3 px-4 py-3"}`}>
          {serversCompact ? (
            <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Expand servers" onClick={() => setServersCompact(false)}>
              <PanelLeftOpen className="h-4 w-4" />
            </Button>
          ) : (
            <>
              <h3 className="flex items-center gap-2 text-sm font-semibold">
                <Server className="h-4 w-4" />
                Targets
              </h3>
              <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse servers" onClick={() => setServersCompact(true)}>
                <PanelLeftClose className="h-4 w-4" />
              </Button>
            </>
          )}
        </div>
        <div className={`grid content-start gap-1 overflow-auto ${serversCompact ? "p-2" : "p-2"}`}>
          {targetItems.map((target) => {
            const serverID = target.connector_kind === "ssh" ? target.server_id : null;
            const server = serverID ? servers.data.find((item) => Number(item.id) === Number(serverID)) : null;
            const session = serverID ? latestSessionForServer(sessions, serverID) || emptySession : emptySession;
            const active = selectedTarget && selectedTarget.ref === target.ref;
            const pendingCount = serverID
              ? pendingApprovals.filter((approval) => Number(approval.server_id) === Number(serverID)).length
              : pendingConnectorApprovals.filter((approval) => approval.target_ref === target.ref).length;
            const runningCount = serverID
              ? approvals.data.filter((approval) => approval.status === "running" && Number(approval.server_id) === Number(serverID)).length
              : connectorActionApprovals.data.filter((approval) => approval.status === "running" && approval.target_ref === target.ref).length;
            const unreadCount = serverID ? unreadMessages.filter((message) => Number(message.server_id) === Number(serverID)).length : 0;
            const attentionCount = pendingCount + unreadCount;
            const status = selectedTargetStatus({ target, session, pendingCount, runningCount });
            const TargetIcon = target.connector_kind === "ssh" ? Server : Database;
            return (
              <button
                type="button"
                title={`${targetDisplayName(target)} ${targetSubtitle(target, server)}`}
                className={`${serversCompact ? "grid h-10 w-10 place-items-center px-0 py-0" : "grid gap-1 px-3 py-2 text-left"} rounded-md transition ${
                  active ? "bg-emerald-950 text-white" : "text-stone-700 hover:bg-stone-100"
                }`}
                key={target.ref}
                onClick={() => selectTarget(target.ref)}
              >
                {serversCompact ? (
                  <span className="relative grid h-full w-full place-items-center">
                    <TargetIcon className="h-4 w-4" />
                    {attentionCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{attentionCount}</CountBadge> : null}
                    <ConsoleStatusDot status={status} className="absolute right-1 top-1 h-2.5 w-2.5" />
                  </span>
                ) : (
                  <>
                    <span className="flex min-w-0 items-center justify-between gap-2">
                      <span className="truncate text-sm font-semibold">{targetDisplayName(target)}</span>
                      <span className="flex shrink-0 items-center gap-1.5">
                        {attentionCount > 0 ? <CountBadge>{attentionCount}</CountBadge> : null}
                        <ConsoleStatusDot status={status} className={active && status === "offline" ? "text-red-200" : ""} />
                      </span>
                    </span>
                    <span className={`truncate text-xs ${active ? "text-emerald-100" : "text-stone-500"}`}>
                      {targetSubtitle(target, server)}
                    </span>
                  </>
                )}
              </button>
            );
          })}
          {targets.state === "ready" && targetItems.length === 0 && !serversCompact ? <Notice>No targets yet.</Notice> : null}
          {targets.state === "error" && !serversCompact ? <Notice tone="bad">{targets.error}</Notice> : null}
        </div>
      </aside>

      <section
        className={`grid min-w-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border shadow-xl ${
          theme === "light" ? "border-stone-200 bg-white" : "border-stone-800 bg-[#1e1e1e]"
        }`}
      >
        <header
          className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b px-4 py-3 ${
            theme === "light" ? "border-stone-200 bg-stone-50 text-stone-950" : "border-stone-700 bg-[#2d2d2d] text-stone-100"
          }`}
        >
          <div className="flex min-w-0 items-center gap-3">
            <ConsoleStatusDot
              status={selectedTargetStatus({
                target: selectedTarget,
                session: selectedSession,
                pendingCount: selectedPendingApprovals.length,
                runningCount: selectedTarget?.connector_kind === "ssh"
                  ? approvals.data.filter((approval) => approval.status === "running" && selectedServer && Number(approval.server_id) === Number(selectedServer.id)).length
                  : connectorActionApprovals.data.filter((approval) => approval.status === "running" && selectedTarget && approval.target_ref === selectedTarget.ref).length,
              })}
            />
            <div className="min-w-0">
              <h3 className="flex min-w-0 items-center gap-2 text-sm font-semibold">
                <TerminalSquare className="h-4 w-4 shrink-0" />
                <span className="truncate">{selectedTarget ? targetDisplayName(selectedTarget) : "Console"}</span>
              </h3>
              {selectedTarget ? (
                <p className={`truncate text-xs ${theme === "light" ? "text-stone-500" : "text-stone-400"}`}>
                  {targetSubtitle(selectedTarget, selectedServer)}
                </p>
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 gap-2">
            {selectedPendingApprovals.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                className="h-9 border border-red-500/70 bg-red-950/30 px-3 text-red-100 hover:bg-red-900/40"
                onClick={() => openApproval(selectedPendingApprovals[0])}
                title="Pending approvals"
              >
                <AlertTriangle className="h-3.5 w-3.5" />
                {selectedPendingApprovals.length}
              </Button>
            ) : null}
            {pendingConnectorApprovals.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                className="h-9 border border-amber-500/70 bg-amber-950/30 px-3 text-amber-100 hover:bg-amber-900/40"
                onClick={() => openConnectorApproval(pendingConnectorApprovals[0])}
                title="Pending connector approvals"
              >
                <AlertTriangle className="h-3.5 w-3.5" />
                {pendingConnectorApprovals.length}
              </Button>
            ) : null}
            <Button
              type="button"
              variant="ghost"
              className={`relative h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`}
              onClick={() => setConnectorActivityOpen(true)}
              title="Recent connector activity"
            >
              <Database className="h-3.5 w-3.5" />
              Connectors
              {connectorActionCount > 0 ? <CountBadge className="absolute -right-1 -top-1" tone="stone">{Math.min(connectorActionCount, 99)}</CountBadge> : null}
            </Button>
            <Button
              type="button"
              variant="ghost"
              className={`relative h-9 border px-3 ${
                selectedUnreadMessages.length > 0
                  ? "border-red-500/70 bg-red-950/30 text-red-100 hover:bg-red-900/40"
                  : theme === "light"
                    ? "border-stone-300 text-stone-800 hover:bg-stone-100"
                    : "border-stone-600 text-stone-100 hover:bg-stone-700"
              }`}
              onClick={() => openMessages()}
              disabled={!selectedServer}
            >
              <MessageSquare className="h-3.5 w-3.5" />
              Messages
              {selectedUnreadMessages.length > 0 ? <CountBadge className="absolute -right-1 -top-1">{selectedUnreadMessages.length}</CountBadge> : null}
            </Button>
            <Button
              type="button"
              variant="ghost"
              className={`h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`}
              onClick={() => setBulkCommandOpen(true)}
              disabled={servers.data.length === 0}
              title="Run one command across selected servers"
            >
              <ListChecks className="h-3.5 w-3.5" />
              Bulk
            </Button>
            <Button
              type="button"
              variant="ghost"
              className={`h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`}
              onClick={() => setFileTransferOpen(true)}
              disabled={!selectedServer}
              title="Upload or download one file"
            >
              <Files className="h-3.5 w-3.5" />
              Files
            </Button>
            <Button
              type="button"
              variant="ghost"
              className={`h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`}
              onClick={() => selectedServer && void newConsoleSession(selectedServer)}
              disabled={!selectedServer}
            >
              <RefreshCcw className="h-3.5 w-3.5" />
              New Session
            </Button>
            <Button
              type="button"
              variant="ghost"
              className={`h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`}
              onClick={() => selectedSession.id && void closeConsoleSession(selectedSession.id)}
              disabled={!selectedSessionLive}
              title="Close the remote shell session"
            >
              <XCircle className="h-3.5 w-3.5" />
              End Session
            </Button>
            <Button
              type="button"
              variant="ghost"
              className={`h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`}
              onClick={() => selectedSession.id && cancelConsoleCommand(selectedSession.id)}
              disabled={!selectedSessionLive || selectedSession.status !== "connected"}
              title="Send Ctrl+C to the running command"
            >
              <Square className="h-3.5 w-3.5" />
              Interrupt
            </Button>
          </div>
        </header>

        <div
          className={consoleBannerCount > 0 ? "grid min-h-0" : "min-h-0"}
          style={consoleBannerCount > 0 ? { gridTemplateRows: `${Array(consoleBannerCount).fill("auto").join(" ")} minmax(0, 1fr)` } : undefined}
        >
          {showAlwaysRunWarning ? (
            <div className="sticky top-0 z-10 border-b border-red-800/50 bg-red-950 px-4 py-2 text-xs font-semibold text-red-50">
              MCP is started and {alwaysRunTokens.length} token{alwaysRunTokens.length === 1 ? "" : "s"} can run commands on this server without approval. Prefer prompt mode unless direct execution is intentional.
              {temporaryAlwaysRunLabels.length > 0 ? ` Temporary grant: ${temporaryAlwaysRunLabels[0]}.` : ""}
            </div>
          ) : null}
          {selectedRunningRequest ? (
            <ConsoleRecoveryPanel
              request={selectedRunningRequest}
              now={now}
              theme={theme}
              action={restartAction}
              onRestart={restartSelectedConsoleSession}
            />
          ) : null}
          {selectedTarget && selectedTarget.connector_kind !== "ssh" ? (
            <StructuredConnectorPanel
              target={selectedTarget}
              approvals={connectorActionApprovals}
              theme={theme}
              onOpenActivity={() => setConnectorActivityOpen(true)}
            />
          ) : selectedServer && selectedSessionLive ? (
            <PtyConsole
              key={selectedSession.id || selectedServer.id}
              server={selectedServer}
              session={selectedSession}
              onInput={(data) => selectedSession.id && sendConsoleInput(selectedSession.id, data)}
              onResize={(cols, rows) => selectedSession.id && resizeConsoleSession(selectedSession.id, cols, rows)}
              theme={theme}
            />
          ) : selectedServer ? (
            <NoLiveSession
              server={selectedServer}
              lastSession={selectedSession.id ? selectedSession : null}
              onNewSession={() => void newConsoleSession(selectedServer)}
              theme={theme}
            />
          ) : (
            <div className={`p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}>Select a target.</div>
          )}
        </div>
      </section>

      <TokenPermissionPanel
        tokens={tokens}
        selectedServer={selectedServer}
        selectedTarget={selectedTarget}
        permissionState={permissionState}
        unreadMessages={unreadMessages}
        compact={tokensCompact}
        onToggleCompact={() => setTokensCompact((current) => !current)}
        onOpenMessages={(tokenID) => openMessages(tokenID)}
        onRefresh={async () => {
          const tokenItems = await loadTokens();
          await loadAllTokenPermissions(tokenItems);
        }}
        onSetRule={setTokenServerRule}
      />

      <ApprovalDialog
        approval={activeApproval}
        note={approvalNote}
        action={approvalAction}
        onNoteChange={setApprovalNote}
        onRun={approveActiveRequest}
        onDecline={declineActiveRequest}
        onClose={closeApprovalDialog}
      />
      <ConnectorActionApprovalDialog
        approval={activeConnectorApproval}
        note={connectorApprovalNote}
        action={connectorApprovalAction}
        onNoteChange={setConnectorApprovalNote}
        onRun={approveActiveConnectorRequest}
        onDecline={declineActiveConnectorRequest}
        onClose={closeConnectorApprovalDialog}
      />
      <ConnectorActivityDialog
        open={connectorActivityOpen}
        approvals={connectorActionApprovals}
        onRefresh={loadConnectorActionApprovals}
        onClose={() => setConnectorActivityOpen(false)}
      />
      <FileTransferDialog
        open={fileTransferOpen}
        server={selectedServer}
        onClose={() => setFileTransferOpen(false)}
      />
      <BulkCommandDialog
        open={bulkCommandOpen}
        servers={servers.data}
        selectedServer={selectedServer}
        onClose={() => setBulkCommandOpen(false)}
        onRefresh={loadApprovals}
      />
      <MessagesDialog
        open={messagesOpen}
        server={selectedServer}
        tokens={selectedTokenOptions}
        tokenID={messageTokenID}
        state={messagesState}
        text={messageText}
        onTokenChange={setMessageTokenID}
        onTextChange={setMessageText}
        onSubmit={sendUserMessage}
        onRefresh={loadServerMessages}
        onClose={closeMessages}
      />
    </section>
  );
}

function StructuredConnectorPanel({ target, approvals, theme, onOpenActivity }) {
  const items = (approvals?.data || []).filter((item) => item.target_ref === target.ref);
  const latest = items[0] || null;
  const panelClass = theme === "light" ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100";
  const mutedClass = theme === "light" ? "text-stone-500" : "text-stone-400";
  const borderClass = theme === "light" ? "border-stone-200" : "border-stone-700";

  return (
    <div className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)] ${panelClass}`}>
      <div className={`border-b ${borderClass} p-4`}>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="neutral">{target.connector_kind}</Badge>
              <Badge tone="neutral">{target.profile_label}</Badge>
              <span className={`font-mono text-xs ${mutedClass}`}>{target.ref}</span>
            </div>
            <h3 className="mt-3 text-base font-semibold">Structured connector target</h3>
            <p className={`mt-1 max-w-3xl text-sm ${mutedClass}`}>
              SSH targets open a live terminal. {target.connector_kind} actions run as structured calls through the same token permission,
              approval, history, and audit pipeline.
            </p>
          </div>
          <Button type="button" variant="outline" onClick={onOpenActivity}>
            <Database className="h-4 w-4" />
            Open activity
          </Button>
        </div>
      </div>
      <div className="min-h-0 overflow-auto p-4">
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
          <section className={`overflow-hidden rounded-lg border ${borderClass}`}>
            <div className={`border-b px-4 py-3 ${theme === "light" ? "border-stone-200 bg-stone-50" : "border-stone-700 bg-[#252526]"}`}>
              <h4 className="text-sm font-semibold">Recent activity</h4>
              <p className={`mt-1 text-xs ${mutedClass}`}>{items.length} action{items.length === 1 ? "" : "s"} recorded for this target profile.</p>
            </div>
            <div className="divide-y divide-stone-200 dark:divide-stone-700">
              {items.slice(0, 8).map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className={`grid w-full gap-1 px-4 py-3 text-left transition ${theme === "light" ? "hover:bg-stone-50" : "hover:bg-stone-800/60"}`}
                  onClick={onOpenActivity}
                >
                  <span className="flex min-w-0 items-center justify-between gap-2">
                    <span className="truncate font-mono text-xs font-semibold">{item.action_name}</span>
                    <ActivityStatusBadge status={item.status} />
                  </span>
                  <span className={`truncate text-xs ${mutedClass}`}>{item.reason || formatConnectorTime(item.created_at)}</span>
                </button>
              ))}
              {items.length === 0 ? <p className={`px-4 py-5 text-sm ${mutedClass}`}>No connector actions yet. AI activity for this target will appear here.</p> : null}
            </div>
          </section>
          <aside className={`grid content-start gap-3 rounded-lg border p-4 ${borderClass}`}>
            <h4 className="text-sm font-semibold">Target profile</h4>
            <dl className="grid gap-3 text-sm">
              <div>
                <dt className={`text-xs uppercase ${mutedClass}`}>Target</dt>
                <dd className="mt-1 font-semibold">{target.target_name}</dd>
              </div>
              <div>
                <dt className={`text-xs uppercase ${mutedClass}`}>Profile</dt>
                <dd className="mt-1 font-semibold">{target.profile_label}</dd>
              </div>
              <div>
                <dt className={`text-xs uppercase ${mutedClass}`}>Endpoint</dt>
                <dd className="mt-1 truncate font-mono text-xs">{targetEndpoint(target)}</dd>
              </div>
            </dl>
            {latest ? (
              <Notice tone={latest.status === "completed" ? "good" : latest.status === "failed" || latest.status === "error" ? "bad" : "warn"}>
                Latest action: {latest.action_name} ({latest.status})
              </Notice>
            ) : null}
          </aside>
        </div>
      </div>
    </div>
  );
}

function ActivityStatusBadge({ status }) {
  const tone = status === "completed" ? "good" : status === "failed" || status === "error" || status === "stale" ? "bad" : status === "approval_pending" || status === "running" ? "warn" : "neutral";
  return <Badge tone={tone}>{status}</Badge>;
}

function ConsoleRecoveryPanel({ request, now, theme, action, onRestart }) {
  const ageMs = Math.max(0, now - parseTimestamp(request.created_at));
  const showRecoveryHint = ageMs >= 20000;
  const panelClass = theme === "light" ? "border-amber-300 bg-amber-50 text-amber-950" : "border-amber-900/70 bg-amber-950/40 text-amber-50";
  const mutedClass = theme === "light" ? "text-amber-900/80" : "text-amber-100/80";
  const commandPreview = firstLine(request.command || "command");
  const sourceLabel = runningRequestLabel(request);

  return (
    <div className={`flex min-h-9 items-center gap-3 border-b px-4 py-2 text-xs ${panelClass}`}>
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Clock className="h-3.5 w-3.5 shrink-0" />
        <span className="shrink-0 font-semibold">{sourceLabel}</span>
        <span className={`shrink-0 rounded-full px-2 py-0.5 ${theme === "light" ? "bg-stone-200 text-stone-700" : "bg-stone-800 text-stone-200"}`}>
          {formatDuration(ageMs)}
        </span>
        {request.token_name ? (
          <span className={`shrink-0 rounded-full px-2 py-0.5 ${theme === "light" ? "bg-emerald-100 text-emerald-800" : "bg-emerald-950 text-emerald-100"}`}>
            {request.token_name}
          </span>
        ) : null}
        <span className={`min-w-0 truncate font-mono ${mutedClass}`}>{commandPreview}</span>
        {showRecoveryHint ? <span className="shrink-0 font-medium">Looks stuck? Restart opens a fresh SSH session.</span> : null}
      </div>
      {action.error ? <span className="max-w-80 truncate text-red-300">{action.error}</span> : null}
      <Button
        type="button"
        variant="outline"
        className={`h-7 shrink-0 px-2 text-xs ${
          theme === "light"
            ? "border-amber-400 bg-amber-100 text-amber-950 hover:bg-amber-200"
            : "border-amber-700 bg-amber-950/70 text-amber-50 hover:bg-amber-900/70"
        }`}
        onClick={onRestart}
        disabled={action.state === "running"}
        title="Close the gateway-owned persistent console session and let the next command open a fresh SSH session"
      >
        <RefreshCcw className="h-3.5 w-3.5" />
        {action.state === "running" ? "Restarting..." : "Restart"}
      </Button>
    </div>
  );
}

function runningRequestLabel(request) {
  if (request?.source === "manual") return "Manual command running";
  if (request?.source === "mcp") return "AI command running";
  return "Command running";
}

function firstLine(value) {
  const line = String(value || "").split(/\r?\n/, 1)[0].trim();
  if (line.length <= 90) return line;
  return `${line.slice(0, 87)}...`;
}

function parseTimestamp(value) {
  const parsed = Date.parse(value || "");
  return Number.isNaN(parsed) ? Date.now() : parsed;
}

function formatDuration(ms) {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}

function formatConnectorTime(value) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
}

function defaultConsoleTargetRef(targets, pendingApprovals, unreadMessages, pendingConnectorApprovals) {
  if (!targets.length) return "";
  const pendingSSH = pendingApprovals.find((approval) => targets.some((target) => target.connector_kind === "ssh" && Number(target.server_id) === Number(approval.server_id)));
  if (pendingSSH) {
    const target = targets.find((item) => item.connector_kind === "ssh" && Number(item.server_id) === Number(pendingSSH.server_id));
    if (target) return target.ref;
  }
  const pendingConnector = pendingConnectorApprovals.find((approval) => targets.some((target) => target.ref === approval.target_ref));
  if (pendingConnector) return pendingConnector.target_ref;
  const unread = unreadMessages.find((message) => targets.some((target) => target.connector_kind === "ssh" && Number(target.server_id) === Number(message.server_id)));
  if (unread) {
    const target = targets.find((item) => item.connector_kind === "ssh" && Number(item.server_id) === Number(unread.server_id));
    if (target) return target.ref;
  }
  return targets[0].ref;
}

function targetDisplayName(target) {
  if (!target) return "Target";
  if (target.connector_kind === "ssh") return target.target_name;
  return `${target.target_name} / ${target.profile_label}`;
}

function targetSubtitle(target, server) {
  if (!target) return "";
  if (target.connector_kind === "ssh") {
    const username = target.public?.username || server?.username || "ssh";
    const host = target.config?.host || server?.host || "host";
    const port = target.config?.port || server?.port || 22;
    return `${username}@${host}:${port}`;
  }
  if (target.connector_kind === "postgres") {
    const host = target.config?.host || "host";
    const port = target.config?.port || 5432;
    const database = target.config?.database || "database";
    return `${host}:${port}/${database}`;
  }
  return `${target.connector_kind} profile ${target.profile_label}`;
}

function targetEndpoint(target) {
  if (!target) return "-";
  if (target.connector_kind === "ssh") return targetSubtitle(target, null);
  if (target.connector_kind === "postgres") return targetSubtitle(target, null);
  return target.ref;
}

function selectedTargetStatus({ target, session, pendingCount = 0, runningCount = 0 }) {
  if (pendingCount > 0 || runningCount > 0) return "busy";
  if (target?.connector_kind && target.connector_kind !== "ssh") return "idle";
  return selectedServerStatus({ session, pendingCount, runningCount });
}

function selectedServerStatus({ session, pendingCount = 0, runningCount = 0 }) {
  if (pendingCount > 0 || runningCount > 0) return "busy";
  if (session?.status === "connected" || session?.status === "connecting") return "idle";
  return "offline";
}

function ConsoleStatusDot({ status, className = "" }) {
  const colors = {
    offline: "fill-red-500 text-red-500",
    idle: "fill-emerald-500 text-emerald-500",
    busy: "fill-amber-400 text-amber-400",
  };
  const label = {
    offline: "No live session",
    idle: "Target ready",
    busy: "Pending or running work",
  };
  const title = label[status] || label.offline;
  return <Circle className={`h-3 w-3 shrink-0 ${colors[status] || colors.offline} ${className}`} aria-label={title} title={title} />;
}
