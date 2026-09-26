import { useMemo } from "react";
import { emptySession, isLiveConsoleSession, isUnreadMessage, latestSessionForRuntime } from "./helpers";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts.ts";

type RuntimeTarget = { id: number };
type Session = { runtime_id?: string | number; status?: string };
type Message = Pick<RuntimeMessage, "runtime_id" | "direction" | "consumed_at">;
type Props<Target extends RuntimeTarget, SessionItem extends Session, MessageItem extends Message> = {
  liveConsoleTargets: { data: Target[] };
  messages: { data: MessageItem[] };
  sessions: SessionItem[];
  selectedRuntimeID: string;
  allowTargetFallback?: boolean;
};

export function useConsolePageState<Target extends RuntimeTarget, SessionItem extends Session, MessageItem extends Message>({
  liveConsoleTargets,
  messages,
  sessions,
  selectedRuntimeID,
  allowTargetFallback = true,
}: Props<Target, SessionItem, MessageItem>) {
  const selectedRuntimeTarget = useMemo(() => {
    if (!liveConsoleTargets.data.length) return null;
    if (!selectedRuntimeID) return allowTargetFallback ? liveConsoleTargets.data[0] : null;
    return (
      liveConsoleTargets.data.find((target) => String(target.id) === selectedRuntimeID) ||
      (allowTargetFallback ? liveConsoleTargets.data[0] : null)
    );
  }, [liveConsoleTargets.data, selectedRuntimeID, allowTargetFallback]);

  const selectedSession = selectedRuntimeTarget
    ? latestSessionForRuntime(sessions, selectedRuntimeTarget.id) || emptySession
    : emptySession;
  const selectedSessionLive = isLiveConsoleSession(selectedSession);
  const unreadMessages = useMemo(() => messages.data.filter(isUnreadMessage), [messages.data]);
  const selectedUnreadMessages = selectedRuntimeTarget
    ? unreadMessages.filter((message) => Number(message.runtime_id) === Number(selectedRuntimeTarget.id))
    : [];

  const defaultRuntimeID = useMemo(() => {
    if (!liveConsoleTargets.data.length) return "";
    const unread = unreadMessages.find((message) =>
      liveConsoleTargets.data.some((target) => Number(target.id) === Number(message.runtime_id)),
    );
    return String(unread ? unread.runtime_id : liveConsoleTargets.data[0].id);
  }, [liveConsoleTargets.data, unreadMessages]);

  return {
    selectedRuntimeTarget,
    selectedSession,
    selectedSessionLive,
    unreadMessages,
    selectedUnreadMessages,
    defaultRuntimeID,
  };
}
