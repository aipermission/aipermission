import { useMemo } from "react";
import { emptySession, isLiveConsoleSession, isUnreadMessage, latestSessionForRuntimeProfile } from "./helpers";

export function useConsolePageState({ liveConsoleTargets, messages, sessions, selectedRuntimeProfileID, allowTargetFallback = true }) {
  const selectedRuntimeTarget = useMemo(() => {
    if (!liveConsoleTargets.data.length) return null;
    if (!selectedRuntimeProfileID) return allowTargetFallback ? liveConsoleTargets.data[0] : null;
    return liveConsoleTargets.data.find((target) => String(target.id) === selectedRuntimeProfileID) || (allowTargetFallback ? liveConsoleTargets.data[0] : null);
  }, [liveConsoleTargets.data, selectedRuntimeProfileID, allowTargetFallback]);

  const selectedSession = selectedRuntimeTarget ? latestSessionForRuntimeProfile(sessions, selectedRuntimeTarget.id) || emptySession : emptySession;
  const selectedSessionLive = isLiveConsoleSession(selectedSession);
  const unreadMessages = messages.data.filter(isUnreadMessage);
  const selectedUnreadMessages = selectedRuntimeTarget ? unreadMessages.filter((message) => Number(message.runtime_profile_id) === Number(selectedRuntimeTarget.id)) : [];

  const defaultRuntimeProfileID = useMemo(() => {
    if (!liveConsoleTargets.data.length) return "";
    const unread = unreadMessages.find((message) => liveConsoleTargets.data.some((target) => Number(target.id) === Number(message.runtime_profile_id)));
    return String(unread ? unread.runtime_profile_id : liveConsoleTargets.data[0].id);
  }, [
    liveConsoleTargets.data,
    unreadMessages.map((message) => `${message.id}:${message.runtime_profile_id}`).join(","),
  ]);

  return {
    selectedRuntimeTarget,
    selectedSession,
    selectedSessionLive,
    unreadMessages,
    selectedUnreadMessages,
    defaultRuntimeProfileID,
  };
}
