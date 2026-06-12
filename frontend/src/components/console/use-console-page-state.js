import { useMemo } from "react";
import { emptySession, isLiveConsoleSession, isUnreadMessage, latestSessionForServer } from "./helpers";

export function useConsolePageState({ servers, approvals, messages, sessions, selectedServerID, allowServerFallback = true }) {
  const selectedServer = useMemo(() => {
    if (!servers.data.length) return null;
    if (!selectedServerID) return allowServerFallback ? servers.data[0] : null;
    return servers.data.find((server) => String(server.id) === selectedServerID) || (allowServerFallback ? servers.data[0] : null);
  }, [servers.data, selectedServerID, allowServerFallback]);

  const selectedSession = selectedServer ? latestSessionForServer(sessions, selectedServer.id) || emptySession : emptySession;
  const selectedSessionLive = isLiveConsoleSession(selectedSession);
  const pendingApprovals = approvals.data.filter((approval) => approval.status === "pending_approval");
  const unreadMessages = messages.data.filter(isUnreadMessage);
  const selectedPendingApprovals = selectedServer ? pendingApprovals.filter((approval) => Number(approval.server_id) === Number(selectedServer.id)) : [];
  const selectedUnreadMessages = selectedServer ? unreadMessages.filter((message) => Number(message.server_id) === Number(selectedServer.id)) : [];

  const defaultServerID = useMemo(() => {
    if (!servers.data.length) return "";
    const pending = pendingApprovals.find((approval) => servers.data.some((server) => Number(server.id) === Number(approval.server_id)));
    if (pending) return String(pending.server_id);
    const unread = unreadMessages.find((message) => servers.data.some((server) => Number(server.id) === Number(message.server_id)));
    return String(unread ? unread.server_id : servers.data[0].id);
  }, [
    servers.data,
    pendingApprovals.map((approval) => `${approval.id}:${approval.server_id}`).join(","),
    unreadMessages.map((message) => `${message.id}:${message.server_id}`).join(","),
  ]);

  return {
    selectedServer,
    selectedSession,
    selectedSessionLive,
    pendingApprovals,
    unreadMessages,
    selectedPendingApprovals,
    selectedUnreadMessages,
    defaultServerID,
  };
}
