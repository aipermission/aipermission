import { useMemo } from "react";
import { emptySession, isLiveConsoleSession, isUnreadMessage, latestSessionForRuntime } from "./helpers";

export function useConsolePageState({ liveConsoleTargets, messages, sessions, selectedRuntimeID, allowTargetFallback = true }) {
  const selectedRuntimeTarget = useMemo(() => {
    if (!liveConsoleTargets.data.length) return null;
    if (!selectedRuntimeID) return allowTargetFallback ? liveConsoleTargets.data[0] : null;
    return liveConsoleTargets.data.find((target) => String(target.id) === selectedRuntimeID) || (allowTargetFallback ? liveConsoleTargets.data[0] : null);
  }, [liveConsoleTargets.data, selectedRuntimeID, allowTargetFallback]);

  const selectedSession = selectedRuntimeTarget ? latestSessionForRuntime(sessions, selectedRuntimeTarget.id) || emptySession : emptySession;
  const selectedSessionLive = isLiveConsoleSession(selectedSession);
  const unreadMessages = messages.data.filter(isUnreadMessage);
  const selectedUnreadMessages = selectedRuntimeTarget ? unreadMessages.filter((message) => Number(message.runtime_id) === Number(selectedRuntimeTarget.id)) : [];

  const defaultRuntimeID = useMemo(() => {
    if (!liveConsoleTargets.data.length) return "";
    const unread = unreadMessages.find((message) => liveConsoleTargets.data.some((target) => Number(target.id) === Number(message.runtime_id)));
    return String(unread ? unread.runtime_id : liveConsoleTargets.data[0].id);
  }, [
    liveConsoleTargets.data,
    unreadMessages.map((message) => `${message.id}:${message.runtime_id}`).join(","),
  ]);

  return {
    selectedRuntimeTarget,
    selectedSession,
    selectedSessionLive,
    unreadMessages,
    selectedUnreadMessages,
    defaultRuntimeID,
  };
}
