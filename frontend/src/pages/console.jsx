import { AlertTriangle, Circle, MessageSquare, PanelLeftClose, PanelLeftOpen, RefreshCcw, Server, Square, TerminalSquare, XCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { apiGet, apiPost } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { useTokenPermissions } from "../lib/use-token-permissions";
import { CountBadge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Notice } from "../components/ui/notice";
import { ApprovalDialog } from "../components/console/approval-dialog";
import { MessagesDialog } from "../components/console/messages-dialog";
import { NoLiveSession } from "../components/console/no-live-session";
import { PtyConsole } from "../components/console/pty-console";
import { TokenPermissionPanel } from "../components/console/token-permission-panel";
import { emptySession, latestSessionForServer } from "../components/console/helpers";
import { useConsolePageState } from "../components/console/use-console-page-state";

export function ConsolePage() {
  const {
    servers,
    tokens,
    approvals,
    messages,
    loadTokens,
    loadMessages,
    markMessagesRead,
    consoleSessions,
    newConsoleSession,
    attachConsoleSession,
    closeConsoleSession,
    cancelConsoleCommand,
    sendConsoleInput,
    resizeConsoleSession,
    runApproval,
    declineApproval,
    mcpRuntime,
    theme,
  } = useGateway();
  const [searchParams, setSearchParams] = useSearchParams();
  const { permissionState, loadAllTokenPermissions, setTokenServerRule } = useTokenPermissions(tokens.data);
  const [activeApprovalID, setActiveApprovalID] = useState(null);
  const [dismissedApprovalIDs, setDismissedApprovalIDs] = useState({});
  const [approvalNote, setApprovalNote] = useState("");
  const [approvalAction, setApprovalAction] = useState({ state: "idle", error: null });
  const [messagesOpen, setMessagesOpen] = useState(false);
  const [messagesState, setMessagesState] = useState({ state: "idle", data: [], error: null });
  const [messageText, setMessageText] = useState("");
  const [messageTokenID, setMessageTokenID] = useState("");
  const [serversCompact, setServersCompact] = useState(false);
  const [tokensCompact, setTokensCompact] = useState(false);

  const selectedServerID = searchParams.get("server");
  const sessions = consoleSessions.data || [];
  const {
    selectedServer,
    selectedSession,
    selectedSessionLive,
    pendingApprovals,
    unreadMessages,
    selectedPendingApprovals,
    selectedUnreadMessages,
    selectedTokenOptions,
    defaultServerID,
  } = useConsolePageState({
    servers,
    tokens,
    approvals,
    messages,
    sessions,
    selectedServerID,
    permissionState,
  });
  const activeApproval = activeApprovalID ? pendingApprovals.find((approval) => Number(approval.id) === Number(activeApprovalID)) : null;
  const alwaysRunTokens = selectedServer
    ? selectedTokenOptions.filter((token) => permissionState.data?.[token.id]?.[selectedServer.id] === "always_run")
    : [];
  const showAlwaysRunWarning = Boolean(mcpRuntime?.data?.enabled && selectedServer && alwaysRunTokens.length > 0);

  useEffect(() => {
    if (servers.data.length === 0) return;
    if (!selectedServerID && (approvals.state !== "ready" || messages.state !== "ready")) return;
    if (!selectedServerID || !servers.data.some((server) => String(server.id) === selectedServerID)) {
      setSearchParams({ server: defaultServerID }, { replace: true });
    }
  }, [servers.data, selectedServerID, defaultServerID, approvals.state, messages.state, setSearchParams]);

  useEffect(() => {
    if (tokens.state !== "ready") return;
    loadAllTokenPermissions(tokens.data);
  }, [tokens.state, tokens.data.map((token) => token.id).join(",")]);

  useEffect(() => {
    if (!selectedServer) return;
    if (selectedSessionLive) {
      attachConsoleSession(selectedSession.id);
    }
  }, [selectedServer?.id, selectedSession.id, selectedSession.status]);

  useEffect(() => {
    if (activeApprovalID && !pendingApprovals.some((approval) => Number(approval.id) === Number(activeApprovalID))) {
      setActiveApprovalID(null);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
      return;
    }
    if (activeApprovalID || selectedPendingApprovals.length === 0) return;
    const next = selectedPendingApprovals.find((approval) => !dismissedApprovalIDs[approval.id]);
    if (next) {
      setActiveApprovalID(next.id);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
    }
  }, [activeApprovalID, selectedPendingApprovals.map((approval) => approval.id).join(","), dismissedApprovalIDs, pendingApprovals.length]);

  function selectServer(serverID) {
    setSearchParams({ server: String(serverID) });
  }

  function openApproval(approval) {
    setActiveApprovalID(approval.id);
    setApprovalNote(approval.user_note || "");
    setApprovalAction({ state: "idle", error: null });
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
    setApprovalNote("");
    setApprovalAction({ state: "idle", error: null });
  }

  async function approveActiveRequest() {
    if (!activeApproval) return;
    setApprovalAction({ state: "running", error: null });
    try {
      await runApproval(activeApproval.id, approvalNote);
      setDismissedApprovalIDs((current) => {
        const next = { ...current };
        delete next[activeApproval.id];
        return next;
      });
      setActiveApprovalID(null);
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
    } catch (error) {
      setApprovalAction({ state: "error", error: error.message });
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
      setApprovalNote("");
      setApprovalAction({ state: "idle", error: null });
    } catch (error) {
      setApprovalAction({ state: "error", error: error.message });
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
                Servers
              </h3>
              <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse servers" onClick={() => setServersCompact(true)}>
                <PanelLeftClose className="h-4 w-4" />
              </Button>
            </>
          )}
        </div>
        <div className={`grid content-start gap-1 overflow-auto ${serversCompact ? "p-2" : "p-2"}`}>
          {servers.data.map((server) => {
            const session = latestSessionForServer(sessions, server.id) || emptySession;
            const active = selectedServer && Number(selectedServer.id) === Number(server.id);
            const pendingCount = pendingApprovals.filter((approval) => Number(approval.server_id) === Number(server.id)).length;
            const runningCount = approvals.data.filter((approval) => approval.status === "running" && Number(approval.server_id) === Number(server.id)).length;
            const unreadCount = unreadMessages.filter((message) => Number(message.server_id) === Number(server.id)).length;
            const attentionCount = pendingCount + unreadCount;
            const status = selectedServerStatus({ session, pendingCount, runningCount });
            return (
              <button
                type="button"
                title={`${server.name} ${server.username}@${server.host}`}
                className={`${serversCompact ? "grid h-10 w-10 place-items-center px-0 py-0" : "grid gap-1 px-3 py-2 text-left"} rounded-md transition ${
                  active ? "bg-emerald-950 text-white" : "text-stone-700 hover:bg-stone-100"
                }`}
                key={server.id}
                onClick={() => selectServer(server.id)}
              >
                {serversCompact ? (
                  <span className="relative grid h-full w-full place-items-center">
                    <Server className="h-4 w-4" />
                    {attentionCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{attentionCount}</CountBadge> : null}
                    <ConsoleStatusDot status={status} className="absolute right-1 top-1 h-2.5 w-2.5" />
                  </span>
                ) : (
                  <>
                    <span className="flex min-w-0 items-center justify-between gap-2">
                      <span className="truncate text-sm font-semibold">{server.name}</span>
                      <span className="flex shrink-0 items-center gap-1.5">
                        {attentionCount > 0 ? <CountBadge>{attentionCount}</CountBadge> : null}
                        <ConsoleStatusDot status={status} className={active && status === "offline" ? "text-red-200" : ""} />
                      </span>
                    </span>
                    <span className={`truncate text-xs ${active ? "text-emerald-100" : "text-stone-500"}`}>
                      {server.username}@{server.host}
                    </span>
                  </>
                )}
              </button>
            );
          })}
          {servers.state === "ready" && servers.data.length === 0 && !serversCompact ? <Notice>No servers yet.</Notice> : null}
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
              status={selectedServerStatus({
                session: selectedSession,
                pendingCount: selectedPendingApprovals.length,
                runningCount: approvals.data.filter((approval) => approval.status === "running" && selectedServer && Number(approval.server_id) === Number(selectedServer.id)).length,
              })}
            />
            <div className="min-w-0">
              <h3 className="flex min-w-0 items-center gap-2 text-sm font-semibold">
                <TerminalSquare className="h-4 w-4 shrink-0" />
                <span className="truncate">{selectedServer ? selectedServer.name : "Console"}</span>
              </h3>
              {selectedServer ? (
                <p className={`truncate text-xs ${theme === "light" ? "text-stone-500" : "text-stone-400"}`}>
                  {selectedServer.username}@{selectedServer.host}:{selectedServer.port}
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

        <div className={showAlwaysRunWarning ? "grid min-h-0 grid-rows-[auto_minmax(0,1fr)]" : "min-h-0"}>
          {showAlwaysRunWarning ? (
            <div className="sticky top-0 z-10 border-b border-red-800/50 bg-red-950 px-4 py-2 text-xs font-semibold text-red-50">
              MCP is started and {alwaysRunTokens.length} token{alwaysRunTokens.length === 1 ? "" : "s"} can run commands on this server without approval. Prefer prompt mode unless direct execution is intentional.
            </div>
          ) : null}
          {selectedServer && selectedSessionLive ? (
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
            <div className={`p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}>Select a server.</div>
          )}
        </div>
      </section>

      <TokenPermissionPanel
        tokens={tokens}
        selectedServer={selectedServer}
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
    idle: "Live session idle",
    busy: "Pending or running work",
  };
  return <Circle className={`h-3 w-3 shrink-0 ${colors[status] || colors.offline} ${className}`} aria-label={label[status] || label.offline} />;
}
