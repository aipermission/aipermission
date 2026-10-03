import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
import { runtimeMessagesResponse } from "../../lib/gateway-contracts/activity-resource-contracts.ts";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts.ts";
import { errorMessage } from "../../lib/errors.ts";
import { isActiveToken } from "../../lib/token-status";
import { useTokenExpiryClock } from "../../lib/use-token-expiry-clock";

type MessageState = { state: "idle" | "loading" | "ready" | "sending" | "error"; data: RuntimeMessage[]; error: string | null };
type Props = {
  loadMessages: () => void | Promise<unknown>;
  markRuntimeMessagesRead: (_runtimeID: number, _messageIDs: readonly number[]) => Promise<unknown>;
  selectedRuntimeTarget: { id: number; name?: string } | null;
  selectedSession: { id?: number };
  selectedSessionLive: boolean;
  selectedTokenOptions: { id: number; name: string; revoked_at?: string | null; expires_at?: string | null }[];
  selectedUnreadMessages: RuntimeMessage[];
};
const idleState: MessageState = { state: "idle", data: [], error: null };

export function useConsoleMessages({
  loadMessages,
  markRuntimeMessagesRead,
  selectedRuntimeTarget,
  selectedSession,
  selectedSessionLive,
  selectedTokenOptions,
  selectedUnreadMessages,
}: Props) {
  const [isOpen, setOpen] = useState(false);
  const [state, setState] = useState(idleState);
  const [text, setText] = useState("");
  const draftRevision = useRef(0);
  const displayedIDs = useRef<number[]>([]);
  const [selectedTokenID, setTokenID] = useState("");
  const handlers = useRef<{
    open?: (_tokenID?: string | number) => void;
    submit?: (_event: Pick<FormEvent<HTMLFormElement>, "preventDefault">) => Promise<void>;
    close?: () => void;
  }>({});
  const now = useTokenExpiryClock(selectedTokenOptions);
  const activeTokenOptions = useMemo(() => selectedTokenOptions.filter((token) => isActiveToken(token, now)), [selectedTokenOptions, now]);
  const selectedToken = activeTokenOptions.find((token) => String(token.id) === selectedTokenID);
  const tokenID = selectedToken ? selectedTokenID : "";
  const runtimeID = selectedRuntimeTarget?.id ? String(selectedRuntimeTarget.id) : "";
  const requests = useRequestGuard(`console-messages:${runtimeID || "none"}`);

  useEffect(() => {
    requests.invalidate("messages");
    requests.invalidate("send");
    requests.invalidate("mark-read");
    draftRevision.current += 1;
    setOpen(false);
    setState(idleState);
    setText("");
    setTokenID("");
    displayedIDs.current = [];
  }, [requests, runtimeID]);

  const load = useCallback(async () => {
    if (!runtimeID) return;
    displayedIDs.current = [];
    const request = requests.begin("messages");
    setState((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet(`/api/messages?runtime_id=${runtimeID}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      setState({ state: "ready", data: runtimeMessagesResponse(data), error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setState({ state: "error", data: [], error: errorMessage(error, "Could not load messages.") });
    } finally {
      request.complete();
    }
  }, [requests, runtimeID]);

  const open = useCallback(
    function openMessages(preferredTokenID: string | number = "") {
      if (handlers.current.open !== openMessages) return;
      displayedIDs.current = [];
      requests.invalidate("mark-read");
      const currentNow = Date.now();
      const currentTokenOptions = activeTokenOptions.filter((token) => isActiveToken(token, currentNow));
      const candidates = [
        preferredTokenID,
        ...selectedUnreadMessages.map((message) => message.token_id),
        tokenID,
        currentTokenOptions[0]?.id,
      ];
      const nextTokenID = candidates.find((id) => currentTokenOptions.some((token) => String(token.id) === String(id)));
      setTokenID(nextTokenID ? String(nextTokenID) : "");
      setOpen(true);
      void load();
    },
    [activeTokenOptions, load, requests, selectedUnreadMessages, tokenID],
  );

  const recordDisplayed = useCallback(
    (messageIDs: readonly number[]) => {
      if (!isOpen || state.state !== "ready") {
        displayedIDs.current = [];
        return;
      }
      const rendered = new Set(messageIDs);
      displayedIDs.current = state.data
        .filter(
          (message) =>
            rendered.has(message.id) &&
            (!tokenID || String(message.token_id) === tokenID) &&
            message.direction === "ai_to_user" &&
            !message.consumed_at,
        )
        .map((message) => message.id);
    },
    [isOpen, state, tokenID],
  );

  const close = useCallback(
    function closeMessages() {
      if (handlers.current.close !== closeMessages) return;
      const messageIDs = displayedIDs.current;
      displayedIDs.current = [];
      requests.invalidate("messages");
      requests.invalidate("send");
      setOpen(false);
      if (!runtimeID || messageIDs.length === 0) return;
      const request = requests.begin("mark-read");
      void markRuntimeMessagesRead(Number(runtimeID), messageIDs)
        .catch((error) => {
          if (request.isCurrent())
            setState((current) => ({ ...current, state: "error", error: errorMessage(error, "Could not mark messages read.") }));
        })
        .finally(request.complete);
    },
    [markRuntimeMessagesRead, requests, runtimeID],
  );

  const updateText = useCallback((value: string) => {
    draftRevision.current += 1;
    setText(value);
  }, []);

  const submit = useCallback(
    async function submitMessage(event: Pick<FormEvent<HTMLFormElement>, "preventDefault">) {
      event.preventDefault();
      if (
        handlers.current.submit !== submitMessage ||
        !runtimeID ||
        !text.trim() ||
        !tokenID ||
        !selectedToken ||
        !isActiveToken(selectedToken)
      )
        return;
      const submittedDraftRevision = draftRevision.current;
      const request = requests.begin("send");
      setState((current) => ({ ...current, state: "sending", error: null }));
      try {
        await apiPost(
          "/api/messages",
          {
            token_id: Number(tokenID),
            runtime_id: Number(runtimeID),
            session_id: selectedSessionLive ? (selectedSession.id ?? null) : null,
            direction: "user_to_ai",
            message: text,
          },
          { signal: request.signal },
        );
        if (!request.isCurrent()) return;
        if (draftRevision.current === submittedDraftRevision) setText("");
        await Promise.allSettled([load(), loadMessages()]);
      } catch (error) {
        if (!request.isCurrent()) return;
        setState((current) => ({ ...current, state: "error", error: errorMessage(error, "Could not send message.") }));
      } finally {
        request.complete();
      }
    },
    [load, loadMessages, requests, runtimeID, selectedSession.id, selectedSessionLive, selectedToken, text, tokenID],
  );

  // Only handlers from the committed view may change selection or dispatch.
  useLayoutEffect(() => {
    handlers.current = { open, submit, close };
    return () => {
      handlers.current = {};
    };
  }, [open, submit, close]);

  return { close, isOpen, load, open, recordDisplayed, setText: updateText, setTokenID, state, submit, text, tokenID };
}
