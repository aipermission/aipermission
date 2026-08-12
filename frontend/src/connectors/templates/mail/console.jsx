import { ChevronRight, CircleCheck, Mail, PenLine, RefreshCcw } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";
import { apiPost } from "../../../lib/api";
import { connectorActionCode, connectorActionError, connectorActionPending } from "../_shared/action-result";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { ComposeDialog } from "./compose-dialog";
import { MailActionResultDialog } from "./action-result-dialog";
import {
  addressValues,
  mailActionResolution,
  mailActionSummary,
  mailFolderAllowed,
  mailProtocolCapabilities,
  messageRefKey,
  replySubject,
  replyText,
  submissionDraftFingerprint,
  unknownSubmissionRetryDecision,
} from "./helpers";
import { FolderPane, MessagePane } from "./mailbox-pane";
import { MessageDetail } from "./message-detail";
import { DeleteMessageDialog, MoveMessageDialog, RetryUnknownSubmissionDialog } from "./message-dialogs";
import { targetEndpoint } from "./model";

const defaultFolder = "INBOX";
const defaultMessageLimit = 50;

export function MailConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const [folders, setFolders] = useState([]);
  const [folderStats, setFolderStats] = useState({});
  const [selectedFolder, setSelectedFolder] = useState(defaultFolder);
  const [messages, setMessages] = useState([]);
  const [selectedMessage, setSelectedMessage] = useState(null);
  const [query, setQuery] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [unreadOnly, setUnreadOnly] = useState(true);
  const [nextCursor, setNextCursor] = useState("");
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const [compose, setCompose] = useState({ open: false, reply: false, form: {} });
  const [pendingActions, setPendingActions] = useState({});
  const [resultDialog, setResultDialog] = useState({ open: false, actionName: "", summary: "", item: null });
  const [moveDialog, setMoveDialog] = useState({ open: false, destination: "", sourceFolder: "" });
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [retryDialog, setRetryDialog] = useState({ open: false, fields: null, messageID: "" });
  const refreshMailboxForEffect = useEffectEvent((options) => refreshMailbox(options));
  const reconcilePendingActionForEffect = useEffectEvent((pending, resolution) => reconcilePendingAction(pending, resolution));
  const requestGeneration = useRef(0);
  const currentTargetRef = useRef(target.ref);

  const {
    panel: panelClass,
    muted: mutedClass,
    border: borderClass,
    subtlePanel: subtlePanelClass,
    input: inputClass,
    rowHover: rowHoverClass,
  } = connectorConsoleTheme(theme);
  const activeRowClass =
    theme === "light" ? "border-emerald-300 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const resultClass =
    theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-900" : "border-emerald-800 bg-emerald-950/40 text-emerald-100";
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );
  const latestAction = activeItems[0] || null;
  const busy = state.state !== "idle" && state.state !== "error";
  const outboundPending =
    Object.values(pendingActions).some((pending) => pending.actionName === "send_message" || pending.actionName === "reply_message") ||
    activeItems.some(
      (item) => ["send_message", "reply_message"].includes(item.action_name) && ["approval_pending", "running"].includes(item.status),
    );
  const composeError = ["send_message", "reply_message"].includes(state.result?.actionName) ? state.error : "";
  const destinationFolders = useMemo(() => allowedDestinationFolders(target), [target]);
  const { imapEnabled, smtpEnabled } = mailProtocolCapabilities(target.public);
  const canArchive = mailFolderAllowed(target.public?.archive_folder, target.public?.allowed_mutation_destination_folders);
  const canDelete = mailFolderAllowed(target.public?.trash_folder, target.public?.allowed_mutation_destination_folders);

  function reportActivityRefreshFailure() {
    setState((current) =>
      current.state === "idle" ? { ...current, error: "Activity refresh unavailable.", message: "", result: null } : current,
    );
  }

  function refreshActivitySafely() {
    try {
      const refresh = onRefreshActivity?.();
      void Promise.resolve(refresh).catch(reportActivityRefreshFailure);
    } catch {
      reportActivityRefreshFailure();
    }
  }

  function openCompose() {
    if (outboundPending) return;
    setState((current) => ({ ...current, error: "" }));
    setCompose({ open: true, reply: false, form: {} });
  }

  function openReply() {
    if (outboundPending) return;
    setState((current) => ({ ...current, error: "" }));
    setCompose({
      open: true,
      reply: true,
      messageRef: selectedMessage?.message_ref,
      form: {
        to: addressValues(selectedMessage?.reply_to?.length ? selectedMessage.reply_to : selectedMessage?.from).join(", "),
        subject: replySubject(selectedMessage?.subject),
        text_body: replyText(selectedMessage),
        html_body: "",
      },
    });
  }

  useEffect(() => {
    requestGeneration.current += 1;
    setFolders([]);
    setFolderStats({});
    setSelectedFolder(defaultFolder);
    setMessages([]);
    setSelectedMessage(null);
    setQuery("");
    setAppliedQuery("");
    setUnreadOnly(true);
    setNextCursor("");
    setState({ state: "idle", error: "", message: "" });
    setCompose({ open: false, reply: false, form: {} });
    setPendingActions({});
    setResultDialog({ open: false, actionName: "", summary: "", item: null });
    setMoveDialog({ open: false, destination: "", sourceFolder: "" });
    setDeleteOpen(false);
    setRetryDialog({ open: false, fields: null, messageID: "" });
  }, [target.ref, activeSession.active, activeSession.startedAt]);

  useEffect(() => {
    currentTargetRef.current = target.ref;
  }, [target.ref]);

  useEffect(() => {
    if (!activeSession.active || !imapEnabled) return;
    void refreshMailboxForEffect({ preferredFolder: defaultFolder, subject: "" });
  }, [activeSession.active, activeSession.startedAt, target.ref, imapEnabled]);

  useEffect(() => {
    const resolved = Object.values(pendingActions)
      .map((pending) => ({ pending, resolution: mailActionResolution(activeItems, pending.requestID) }))
      .filter(({ resolution }) => resolution && resolution.state !== "pending");
    if (resolved.length === 0) return;
    setPendingActions((current) => {
      const next = { ...current };
      for (const { pending } of resolved) delete next[pending.requestID];
      return next;
    });
    for (const { pending, resolution } of resolved) {
      void reconcilePendingActionForEffect(pending, resolution);
    }
  }, [activeItems, pendingActions]);

  async function runMailAction(actionName, input, reason, busyState = "running", pendingContext = {}) {
    const generation = ++requestGeneration.current;
    const targetRef = target.ref;
    setState({ state: busyState, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", { target_ref: targetRef, action_name: actionName, input, reason });
      if (generation !== requestGeneration.current || targetRef !== currentTargetRef.current) return null;
      const actionError = connectorActionError(item);
      if (actionError) {
        const summary = actionError;
        const result = { actionName, summary, item };
        setState({ state: "error", error: actionError, message: "", result });
        if (item?.output) setResultDialog({ open: true, ...result });
        const failure = new Error(actionError);
        failure.actionItem = item;
        failure.actionResult = result;
        throw failure;
      }
      if (connectorActionPending(item)) {
        setPendingActions((current) => ({
          ...current,
          [item.id]: { requestID: item.id, actionName, context: pendingContext, generation },
        }));
        setState({
          state: "idle",
          error: "",
          message: item.display_text || "Mail action is awaiting approval.",
          result: { actionName, summary: item.display_text || "Mail action is awaiting approval.", item },
        });
        refreshActivitySafely();
        return item;
      }
      const summary = mailActionSummary(actionName, item);
      setState({ state: "idle", error: "", message: summary, result: { actionName, summary, item } });
      refreshActivitySafely();
      return item;
    } catch (error) {
      if (generation === requestGeneration.current && targetRef === currentTargetRef.current) {
        setState({
          state: "error",
          error: error.message || "Mail action failed.",
          message: "",
          result: error.actionResult || { actionName, summary: error.message || "Mail action failed.", item: null },
        });
      }
      throw error;
    }
  }

  async function reconcilePendingAction(pending, resolution) {
    const { actionName, context, generation } = pending;
    const { item } = resolution;
    if (["list_folders", "search_messages", "get_message"].includes(actionName) && generation !== requestGeneration.current) {
      return;
    }
    if (resolution.state !== "completed") {
      const fallback = `${String(actionName || "Mail action").replaceAll("_", " ")} was not approved or could not be completed.`;
      const summary = item.error || item.display_text || fallback;
      setState({ state: "error", error: connectorActionError(item, fallback), message: "", result: { actionName, summary, item } });
      if (actionName === "get_message" && connectorActionCode(item) === "stale_message_reference") {
        setSelectedMessage(null);
      }
      if (actionName === "send_message" || actionName === "reply_message") {
        const submissionUnknown =
          item.output?.submission_status === "submission_unknown"
            ? { messageID: item.output.message_id || "", fingerprint: context.draftFingerprint }
            : null;
        setCompose({ open: true, reply: context.reply, messageRef: context.messageRef, form: context.fields || {}, submissionUnknown });
      }
      return;
    }

    const summary = mailActionSummary(actionName, item);
    setState({ state: "idle", error: "", message: summary, result: { actionName, summary, item } });
    switch (actionName) {
      case "list_folders": {
        const preferred = applyFolderResult(item, context.preferredFolder);
        await loadMessages(preferred, { reset: true, subject: context.subject });
        break;
      }
      case "search_messages":
        applyMessageSearchResult(item, context);
        break;
      case "get_message":
        setSelectedMessage(item.output || null);
        break;
      case "mark_read":
      case "mark_unread":
        applyReadStateResult(item, context);
        break;
      case "move_message":
      case "archive_message":
      case "delete_message":
        applyMoveResult(context);
        await loadMessages(context.folder, { reset: true });
        break;
      case "send_message":
      case "reply_message":
        setCompose({ open: false, reply: false, form: {} });
        setRetryDialog({ open: false, fields: null, messageID: "" });
        break;
      default:
        break;
    }
  }

  function applyFolderResult(item, preferredFolder) {
    const nextFolders = Array.isArray(item.output?.folders) ? item.output.folders.filter((folder) => folder.selectable !== false) : [];
    setFolders(nextFolders);
    const preferred = nextFolders.some((folder) => folder.name === preferredFolder)
      ? preferredFolder
      : nextFolders.find((folder) => folder.name.toUpperCase() === defaultFolder)?.name || nextFolders[0]?.name || defaultFolder;
    setSelectedFolder(preferred);
    return preferred;
  }

  function applyMessageSearchResult(item, context) {
    const nextMessages = Array.isArray(item.output?.messages) ? item.output.messages : [];
    setMessages((current) => (context.reset ? nextMessages : mergeMessages(current, nextMessages)));
    setFolderStats((current) => ({
      ...current,
      [context.folder]: { total: Number(item.output?.total || 0), unread: Number(item.output?.unread || 0) },
    }));
    setNextCursor(item.output?.next_cursor || "");
    if (context.reset) setSelectedMessage(null);
  }

  function applyReadStateResult(item, context) {
    const read = Boolean(item.output?.read);
    setSelectedMessage((current) => (messageRefKey(current) === context.messageKey ? { ...current, read } : current));
    setMessages((current) => current.map((message) => (messageRefKey(message) === context.messageKey ? { ...message, read } : message)));
    setFolderStats((current) => updateUnreadCount(current, context.folder, context.wasRead, read));
  }

  function applyMoveResult(context) {
    setMessages((current) => current.filter((message) => messageRefKey(message) !== context.messageKey));
    setSelectedMessage((current) => (messageRefKey(current) === context.messageKey ? null : current));
    setMoveDialog({ open: false, destination: "", sourceFolder: "" });
    setDeleteOpen(false);
  }

  async function refreshMailbox({ preferredFolder = selectedFolder, subject = appliedQuery } = {}) {
    if (!activeSession.active || !imapEnabled) return;
    try {
      const folderItem = await runMailAction("list_folders", {}, "manual Mail workspace folder list", "loading", {
        preferredFolder,
        subject,
      });
      if (!folderItem || connectorActionPending(folderItem)) return;
      const preferred = applyFolderResult(folderItem, preferredFolder);
      await loadMessages(preferred, { reset: true, subject });
    } catch {
      // The shared state already exposes the bounded connector error.
    }
  }

  async function loadMessages(folder, { reset = true, cursor = "", unread = unreadOnly, subject = appliedQuery } = {}) {
    if (!activeSession.active || !folder) return;
    try {
      const item = await runMailAction(
        "search_messages",
        { folder, unread_only: unread, subject, limit: defaultMessageLimit, cursor },
        "manual Mail workspace message search",
        "loading",
        { folder, reset, cursor, unread, subject },
      );
      if (!item || connectorActionPending(item)) return;
      applyMessageSearchResult(item, { folder, reset });
    } catch {
      // The shared state already exposes the bounded connector error.
    }
  }

  async function selectFolder(folder) {
    if (folder === selectedFolder || state.state === "sending" || state.state === "updating") return;
    setSelectedFolder(folder);
    setMessages([]);
    setSelectedMessage(null);
    setNextCursor("");
    await loadMessages(folder, { reset: true });
  }

  async function selectMessage(message) {
    if (busy) return;
    try {
      const item = await runMailAction(
        "get_message",
        { message_ref: message.message_ref },
        "manual Mail workspace message read",
        "reading",
        { messageKey: messageRefKey(message) },
      );
      if (item && !connectorActionPending(item)) setSelectedMessage(item.output || null);
    } catch (error) {
      if (connectorActionCode(error.actionItem) === "stale_message_reference") setSelectedMessage(null);
      // The shared state already exposes the bounded connector error.
    }
  }

  async function toggleRead() {
    if (!selectedMessage) return;
    const actionName = selectedMessage.read ? "mark_unread" : "mark_read";
    try {
      const context = { messageKey: messageRefKey(selectedMessage), folder: selectedFolder, wasRead: selectedMessage.read };
      const item = await runMailAction(
        actionName,
        { message_ref: selectedMessage.message_ref },
        `manual Mail workspace ${actionName.replace("_", " ")}`,
        "updating",
        context,
      );
      if (!item || connectorActionPending(item)) return;
      applyReadStateResult(item, context);
    } catch {
      // The shared state already exposes the bounded connector error.
    }
  }

  async function moveSelected(actionName, destination = "") {
    if (!selectedMessage) return;
    const input = { message_ref: selectedMessage.message_ref };
    if (destination) input.destination_folder = destination;
    try {
      const context = { messageKey: messageRefKey(selectedMessage), folder: selectedFolder };
      const item = await runMailAction(actionName, input, `manual Mail workspace ${actionName.replaceAll("_", " ")}`, "updating", context);
      if (!item || connectorActionPending(item)) return;
      applyMoveResult(context);
      await loadMessages(selectedFolder, { reset: true });
    } catch {
      // Dialog remains available for a deliberate retry.
    }
  }

  async function submitMessage(fields, retryConfirmed = false) {
    const actionName = compose.reply ? "reply_message" : "send_message";
    const draftFingerprint = submissionDraftFingerprint(fields);
    const retryDecision = unknownSubmissionRetryDecision(compose.submissionUnknown, fields);
    if (retryDecision.required && !retryConfirmed) {
      setRetryDialog({ open: true, fields, messageID: compose.submissionUnknown.messageID || "", draftChanged: retryDecision.changed });
      return;
    }
    const input = { ...fields };
    if (compose.reply && compose.messageRef) input.message_ref = compose.messageRef;
    try {
      const pendingContext = { fields, reply: compose.reply, messageRef: compose.messageRef, draftFingerprint };
      const item = await runMailAction(
        actionName,
        input,
        compose.reply ? "manual Mail workspace reply" : "manual Mail workspace send",
        "sending",
        pendingContext,
      );
      if (!item) return;
      if (connectorActionPending(item)) {
        setCompose((current) => ({ ...current, form: fields, pendingRequestID: item.id }));
        return;
      }
      setCompose({ open: false, reply: false, form: {} });
      setRetryDialog({ open: false, fields: null, messageID: "" });
    } catch (error) {
      if (error.actionItem?.output?.submission_status === "submission_unknown") {
        setCompose((current) => ({
          ...current,
          submissionUnknown: { messageID: error.actionItem.output.message_id || "", fingerprint: draftFingerprint },
        }));
      }
      // Keep the draft open so the operator can inspect the exact failure.
    }
  }

  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <div className="grid place-items-center p-8 text-center">
          <div className="grid max-w-lg gap-4">
            <Mail className={`mx-auto h-10 w-10 ${mutedClass}`} />
            <div>
              <h3 className="text-lg font-semibold">No active Mail session</h3>
              <p className={`mt-2 text-sm ${mutedClass}`}>
                Start a structured session to browse bounded IMAP content and submit guarded SMTP actions.
              </p>
            </div>
            <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
              Start Mail session
            </Button>
          </div>
        </div>
        <MailEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] ${panelClass}`}>
      <div className={`flex min-w-0 items-center justify-between gap-3 border-b px-3 py-2 ${borderClass}`}>
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-semibold">{target.public?.mailbox_address || target.profile_label}</span>
          {latestAction ? (
            <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
              {latestAction.action_name}
            </Badge>
          ) : null}
          {state.message ? (
            <button
              type="button"
              className={`flex h-7 max-w-72 items-center gap-1.5 overflow-hidden rounded-md border px-2 text-left text-xs ${resultClass}`}
              onClick={() => state.result && setResultDialog({ open: true, ...state.result })}
              title={state.message}
            >
              <CircleCheck className="h-3.5 w-3.5 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{state.message}</span>
              <ChevronRight className="h-3.5 w-3.5 shrink-0" />
            </button>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          {imapEnabled ? (
            <Button
              type="button"
              variant="outline"
              className="h-8 w-8 px-0"
              title="Refresh mailbox"
              aria-label="Refresh mailbox"
              onClick={() => refreshMailbox()}
              disabled={busy}
            >
              <RefreshCcw className={`h-4 w-4 ${state.state === "loading" ? "animate-spin" : ""}`} />
            </Button>
          ) : null}
          <Button type="button" className="h-8" onClick={openCompose} disabled={busy || outboundPending || !smtpEnabled}>
            <PenLine className="h-4 w-4" />
            Compose
          </Button>
        </div>
      </div>
      {imapEnabled ? (
        <div className="grid min-h-0 gap-2 overflow-y-auto xl:grid-cols-[220px_340px_minmax(0,1fr)] xl:gap-0 xl:overflow-hidden [&>*]:min-h-[320px] xl:[&>*]:min-h-0">
          <FolderPane
            folders={folders}
            selectedFolder={selectedFolder}
            folderStats={folderStats}
            onSelect={selectFolder}
            borderClass={borderClass}
            mutedClass={mutedClass}
            rowHoverClass={rowHoverClass}
            activeRowClass={activeRowClass}
          />
          <MessagePane
            messages={messages}
            selectedRef={messageRefKey(selectedMessage)}
            query={query}
            unreadOnly={unreadOnly}
            hasMore={Boolean(nextCursor)}
            busy={busy}
            onQuery={setQuery}
            onUnreadOnly={(value) => {
              setUnreadOnly(value);
              if (!busy) void loadMessages(selectedFolder, { reset: true, unread: value });
            }}
            onSearch={(event) => {
              event.preventDefault();
              if (!busy) {
                const subject = query.trim();
                setAppliedQuery(subject);
                void loadMessages(selectedFolder, { reset: true, subject });
              }
            }}
            onSelect={selectMessage}
            onLoadMore={() => loadMessages(selectedFolder, { reset: false, cursor: nextCursor })}
            borderClass={borderClass}
            mutedClass={mutedClass}
            inputClass={inputClass}
            rowHoverClass={rowHoverClass}
            activeRowClass={activeRowClass}
          />
          <MessageDetail
            message={selectedMessage}
            busy={busy}
            canReply={smtpEnabled && !outboundPending}
            canMove={destinationFolders.length > 0}
            canArchive={canArchive}
            canDelete={canDelete}
            onToggleRead={toggleRead}
            onReply={openReply}
            onMove={() => setMoveDialog({ open: true, destination: "", sourceFolder: selectedMessage?.message_ref?.folder || "" })}
            onArchive={() => moveSelected("archive_message")}
            onDelete={() => setDeleteOpen(true)}
            borderClass={borderClass}
            mutedClass={mutedClass}
            subtlePanelClass={subtlePanelClass}
          />
        </div>
      ) : (
        <div className="grid min-h-0 place-items-center p-8 text-center">
          <div className={`grid max-w-lg gap-3 rounded-md border p-6 ${borderClass} ${subtlePanelClass}`}>
            <PenLine className={`mx-auto h-9 w-9 ${mutedClass}`} />
            <h3 className="text-base font-semibold">SMTP-only Mail profile</h3>
            <p className={`text-sm ${mutedClass}`}>
              Mailbox browsing is disabled for this profile. Compose remains available through the configured SMTP connection.
            </p>
            <Button type="button" className="mx-auto" onClick={openCompose} disabled={busy || outboundPending || !smtpEnabled}>
              <PenLine className="h-4 w-4" />
              Compose message
            </Button>
          </div>
        </div>
      )}
      <div className={`grid gap-2 border-t px-3 py-2 ${borderClass}`}>
        {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
        <MailEndpointFooter target={target} mutedClass={mutedClass} />
      </div>
      <ComposeDialog
        draft={compose}
        busy={state.state === "sending" || Boolean(compose.pendingRequestID)}
        error={compose.open ? composeError : ""}
        onClose={() =>
          setCompose((current) => (current.pendingRequestID ? { ...current, open: false } : { open: false, reply: false, form: {} }))
        }
        onSubmit={submitMessage}
      />
      <MailActionResultDialog
        value={resultDialog}
        onClose={() => setResultDialog({ open: false, actionName: "", summary: "", item: null })}
      />
      <MoveMessageDialog
        dialog={moveDialog}
        folders={destinationFolders}
        busy={state.state === "updating"}
        onClose={() => setMoveDialog({ open: false, destination: "", sourceFolder: "" })}
        onDestination={(destination) => setMoveDialog((current) => ({ ...current, destination }))}
        onConfirm={() => moveSelected("move_message", moveDialog.destination)}
      />
      <DeleteMessageDialog
        open={deleteOpen}
        trashFolder={target.public?.trash_folder}
        busy={state.state === "updating"}
        onClose={() => setDeleteOpen(false)}
        onConfirm={() => moveSelected("delete_message")}
      />
      <RetryUnknownSubmissionDialog
        value={retryDialog}
        busy={state.state === "sending"}
        onClose={() => setRetryDialog({ open: false, fields: null, messageID: "" })}
        onConfirm={() => retryDialog.fields && submitMessage(retryDialog.fields, true)}
      />
    </div>
  );
}

function MailEndpointFooter({ target, borderClass = "", mutedClass }) {
  return (
    <div className={`flex min-w-0 items-center justify-between gap-3 ${borderClass}`}>
      <span className={`truncate font-mono text-xs ${mutedClass}`}>{target.ref}</span>
      <span className={`truncate text-xs ${mutedClass}`}>{targetEndpoint({ target })}</span>
    </div>
  );
}

function mergeMessages(current, next) {
  const merged = new Map(current.map((message) => [messageRefKey(message), message]));
  for (const message of next) merged.set(messageRefKey(message), message);
  return [...merged.values()];
}

function updateUnreadCount(stats, folder, wasRead, isRead) {
  if (wasRead === isRead) return stats;
  const current = stats[folder] || { total: 0, unread: 0 };
  return { ...stats, [folder]: { ...current, unread: Math.max(0, current.unread + (isRead ? -1 : 1)) } };
}

function allowedDestinationFolders(target) {
  const allowed = Array.isArray(target.public?.allowed_mutation_destination_folders)
    ? target.public.allowed_mutation_destination_folders
    : [];
  return allowed.map((name) => ({ name, display_name: name, selectable: true }));
}
