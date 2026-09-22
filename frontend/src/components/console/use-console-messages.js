import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";

const idleState = { state: "idle", data: [], error: null };

export function useConsoleMessages({
  loadMessages,
  markRuntimeMessagesRead,
  selectedRuntimeTarget,
  selectedSession,
  selectedSessionLive,
  selectedTokenOptions,
  selectedUnreadMessages,
}) {
  const [isOpen, setOpen] = useState(false);
  const [state, setState] = useState(idleState);
  const [text, setText] = useState("");
  const draftRevision = useRef(0);
  const [tokenID, setTokenID] = useState("");
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
  }, [requests, runtimeID]);

  const load = useCallback(async () => {
    if (!runtimeID) return;
    const request = requests.begin("messages");
    setState((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet(`/api/messages?runtime_id=${runtimeID}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      setState({ state: "ready", data, error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setState({ state: "error", data: [], error: error.message });
    } finally {
      request.complete();
    }
  }, [requests, runtimeID]);

  const open = useCallback(
    (preferredTokenID = "") => {
      const unreadToken = selectedUnreadMessages[0]?.token_id;
      const firstToken = selectedTokenOptions[0];
      const nextTokenID = preferredTokenID || unreadToken || tokenID || firstToken?.id || "";
      setTokenID(nextTokenID ? String(nextTokenID) : "");
      setOpen(true);
      void load();
    },
    [load, selectedTokenOptions, selectedUnreadMessages, tokenID],
  );

  const close = useCallback(() => {
    requests.invalidate("messages");
    requests.invalidate("send");
    setOpen(false);
    if (!runtimeID || selectedUnreadMessages.length === 0) return;
    const request = requests.begin("mark-read");
    void markRuntimeMessagesRead(Number(runtimeID))
      .catch((error) => {
        if (request.isCurrent()) setState((current) => ({ ...current, state: "error", error: error.message }));
      })
      .finally(request.complete);
  }, [markRuntimeMessagesRead, requests, runtimeID, selectedUnreadMessages.length]);

  const updateText = useCallback((value) => {
    draftRevision.current += 1;
    setText(value);
  }, []);

  const submit = useCallback(
    async (event) => {
      event.preventDefault();
      if (!runtimeID || !text.trim() || !tokenID) return;
      const submittedDraftRevision = draftRevision.current;
      const request = requests.begin("send");
      setState((current) => ({ ...current, state: "sending", error: null }));
      try {
        await apiPost(
          "/api/messages",
          {
            token_id: Number(tokenID),
            runtime_id: Number(runtimeID),
            session_id: selectedSessionLive ? selectedSession.id : null,
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
        setState((current) => ({ ...current, state: "error", error: error.message }));
      } finally {
        request.complete();
      }
    },
    [load, loadMessages, requests, runtimeID, selectedSession.id, selectedSessionLive, text, tokenID],
  );

  return { close, isOpen, load, open, setText: updateText, setTokenID, state, submit, text, tokenID };
}
