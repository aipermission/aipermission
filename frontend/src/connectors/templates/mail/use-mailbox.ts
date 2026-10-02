import { useEffect, useEffectEvent, useRef, useState } from "react";
import { connectorActionCode, connectorActionPending } from "../_shared/action-result";
import { messageRefKey } from "./helpers";
import { isStaleMessageFailure, MailActionFailure } from "./use-mail-action-runner";
import { readMailFolders, readMailMessage, readMailRecord, readMailSearch } from "./message-output";
import type { MailActionItem, MailActionResolution, MailPendingAction, MailPendingContext, RunMailAction } from "./action-types";
import type { MailFolder, MailFolderStats, MailMessage } from "./message-types";

interface MailMailboxProps {
  scopeKey: string;
  activeSession: { active: boolean; startedAt?: string };
  imapEnabled: boolean;
  busy: boolean;
  folderSelectionLocked: boolean;
  runMailAction: RunMailAction;
}
type SearchOptions = { reset?: boolean; cursor?: string; unread?: boolean; subject?: string };
type RefreshOptions = { preferredFolder?: string; subject?: string };

export const defaultMailFolder = "INBOX";
const defaultMessageLimit = 50;

export function useMailMailbox({ scopeKey, activeSession, imapEnabled, busy, folderSelectionLocked, runMailAction }: MailMailboxProps) {
  const [folders, setFolders] = useState<MailFolder[]>([]);
  const [folderStats, setFolderStats] = useState<Record<string, MailFolderStats>>({});
  const [selectedFolder, setSelectedFolder] = useState(defaultMailFolder);
  const [messages, setMessages] = useState<MailMessage[]>([]);
  const [selectedMessage, setSelectedMessage] = useState<MailMessage | null>(null);
  const [query, setQuery] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [unreadOnly, setUnreadOnly] = useState(true);
  const [nextCursor, setNextCursor] = useState("");
  const [moveDialog, setMoveDialog] = useState({ open: false, destination: "", sourceFolder: "" });
  const [deleteOpen, setDeleteOpen] = useState(false);
  const viewOwner = useRef({});
  const completionOwners = useRef(new WeakMap<MailPendingContext, object>());
  const retiredMessageKeys = useRef(new Set<string>());
  const refreshForEffect = useEffectEvent((options: RefreshOptions) => refreshMailbox(options));

  useEffect(() => {
    viewOwner.current = {};
    retiredMessageKeys.current.clear();
    setFolders([]);
    setFolderStats({});
    setSelectedFolder(defaultMailFolder);
    setMessages([]);
    setSelectedMessage(null);
    setQuery("");
    setAppliedQuery("");
    setUnreadOnly(true);
    setNextCursor("");
    setMoveDialog({ open: false, destination: "", sourceFolder: "" });
    setDeleteOpen(false);
    return () => {
      viewOwner.current = {};
    };
  }, [scopeKey]);

  useEffect(() => {
    if (!activeSession.active || !imapEnabled) return;
    void refreshForEffect({ preferredFolder: defaultMailFolder, subject: "" });
  }, [activeSession.active, activeSession.startedAt, scopeKey, imapEnabled]);

  async function refreshMailbox({ preferredFolder = selectedFolder, subject = appliedQuery }: RefreshOptions = {}) {
    if (!activeSession.active || !imapEnabled) return;
    const context = ownCompletion({ preferredFolder, subject });
    try {
      const item = await runMailAction("list_folders", {}, "manual Mail workspace folder list", "loading", context);
      if (!item || connectorActionPending(item) || !ownsView(context)) return;
      const preferred = applyFolderResult(item, preferredFolder);
      await loadMessages(preferred, { reset: true, subject });
    } catch {
      // The action runner owns the bounded connector error.
    }
  }

  async function loadMessages(
    folder: string,
    { reset = true, cursor = "", unread = unreadOnly, subject = appliedQuery }: SearchOptions = {},
  ) {
    if (!activeSession.active || !folder) return;
    const context = ownCompletion({ folder, reset, cursor, unread, subject });
    try {
      const item = await runMailAction(
        "search_messages",
        { folder, unread_only: unread, subject, limit: defaultMessageLimit, cursor },
        "manual Mail workspace message search",
        "loading",
        context,
      );
      if (!item || connectorActionPending(item) || !ownsView(context)) return;
      applyMessageSearchResult(item, context);
    } catch {
      // The action runner owns the bounded connector error.
    }
  }

  async function selectFolder(folder: string) {
    if (folder === selectedFolder || folderSelectionLocked) return;
    viewOwner.current = {};
    setSelectedFolder(folder);
    setMessages([]);
    setSelectedMessage(null);
    setNextCursor("");
    await loadMessages(folder, { reset: true });
  }

  async function selectMessage(message: MailMessage) {
    if (busy) return;
    const context = ownCompletion({ messageKey: messageRefKey(message) });
    try {
      const item = await runMailAction(
        "get_message",
        { message_ref: message.message_ref },
        "manual Mail workspace message read",
        "reading",
        context,
      );
      if (item && !connectorActionPending(item) && ownsView(context)) applyMessageResult(item);
    } catch (error) {
      if (ownsView(context) && error instanceof MailActionFailure && connectorActionCode(error.actionItem) === "stale_message_reference")
        clearSelectedMessage(context);
    }
  }

  async function toggleRead() {
    if (!selectedMessage) return;
    const actionName = selectedMessage.read ? "mark_unread" : "mark_read";
    const context = ownCompletion({ messageKey: messageRefKey(selectedMessage), folder: selectedFolder, wasRead: selectedMessage.read });
    try {
      const item = await runMailAction(
        actionName,
        { message_ref: selectedMessage.message_ref },
        `manual Mail workspace ${actionName.replace("_", " ")}`,
        "updating",
        context,
      );
      if (item && !connectorActionPending(item)) applyReadStateResult(item, context);
    } catch {
      // The action runner owns the bounded connector error.
    }
  }

  async function moveSelected(actionName: string, destination = "") {
    if (!selectedMessage) return;
    const input: Record<string, unknown> = { message_ref: selectedMessage.message_ref };
    if (destination) input.destination_folder = destination;
    const context = ownCompletion({ messageKey: messageRefKey(selectedMessage), folder: selectedFolder });
    try {
      const item = await runMailAction(actionName, input, `manual Mail workspace ${actionName.replaceAll("_", " ")}`, "updating", context);
      if (!item || connectorActionPending(item)) return;
      // Immediate results have already passed the runner's current-request guard.
      await completeMove(context, true);
    } catch {
      // Confirmation remains available for a deliberate retry.
    }
  }

  async function resolvePending(pending: MailPendingAction, resolution: MailActionResolution, ownsRunner: boolean) {
    const { actionName, context } = pending;
    if (ownsView(context) && isStaleMessageFailure(resolution)) clearSelectedMessage(context);
    if (resolution.state !== "completed") return;
    const { item } = resolution;
    if (["list_folders", "search_messages", "get_message"].includes(actionName) && !ownsView(context)) return;
    if (actionName === "list_folders") {
      const preferred = applyFolderResult(item, context.preferredFolder || selectedFolder);
      await loadMessages(preferred, { reset: true, subject: context.subject });
    } else if (actionName === "search_messages") {
      applyMessageSearchResult(item, context);
    } else if (actionName === "get_message") {
      applyMessageResult(item);
    } else if (actionName === "mark_read" || actionName === "mark_unread") {
      applyReadStateResult(item, context);
    } else if (["move_message", "archive_message", "delete_message"].includes(actionName)) {
      await completeMove(context, ownsRunner);
    }
  }

  // Context identity keeps ownership local without changing action inputs or contracts.
  function ownCompletion(context: MailPendingContext) {
    viewOwner.current = {};
    completionOwners.current.set(context, viewOwner.current);
    return context;
  }

  function ownsView(context: MailPendingContext) {
    return completionOwners.current.get(context) === viewOwner.current;
  }

  function clearSelectedMessage(context: MailPendingContext) {
    setSelectedMessage((current) => (messageRefKey(current) === context.messageKey ? null : current));
  }

  async function completeMove(context: MailPendingContext, ownsRunner: boolean) {
    // Acknowledged mutations retire the exact reference even in a newer view.
    if (context.messageKey) retiredMessageKeys.current.add(context.messageKey);
    setMessages((current) => current.filter((message) => messageRefKey(message) !== context.messageKey));
    clearSelectedMessage(context);
    setFolderStats((current) => {
      const next = { ...current };
      if (context.folder) delete next[context.folder];
      return next;
    });
    if (!ownsRunner || !ownsView(context)) return;
    setMoveDialog({ open: false, destination: "", sourceFolder: "" });
    setDeleteOpen(false);
    await loadMessages(context.folder || selectedFolder, { reset: true });
  }

  function applyFolderResult(item: MailActionItem, preferredFolder: string) {
    const nextFolders = readMailFolders(item.output);
    setFolders(nextFolders);
    const preferred = nextFolders.some((folder) => folder.name === preferredFolder)
      ? preferredFolder
      : nextFolders.find((folder) => folder.name.toUpperCase() === defaultMailFolder)?.name || nextFolders[0]?.name || defaultMailFolder;
    setSelectedFolder(preferred);
    return preferred;
  }

  function isRetiredMessage(message: MailMessage | null) {
    return message !== null && retiredMessageKeys.current.has(messageRefKey(message));
  }

  function applyMessageResult(item: MailActionItem) {
    const message = readMailMessage(item.output);
    if (!isRetiredMessage(message)) setSelectedMessage(message);
  }

  function applyMessageSearchResult(item: MailActionItem, context: MailPendingContext) {
    const output = readMailSearch(item.output);
    const nextMessages = output.messages.filter((message) => !isRetiredMessage(message));
    setMessages((current) => (context.reset ? nextMessages : mergeMessages(current, nextMessages)));
    setFolderStats((current) => ({
      ...current,
      [context.folder || selectedFolder]: { total: output.total, unread: output.unread },
    }));
    setNextCursor(output.nextCursor);
    if (context.reset) setSelectedMessage(null);
  }

  function applyReadStateResult(item: MailActionItem, context: MailPendingContext) {
    const read = Boolean(readMailRecord(item.output).read);
    setSelectedMessage((current) => (messageRefKey(current) === context.messageKey ? { ...current, read } : current));
    setMessages((current) => current.map((message) => (messageRefKey(message) === context.messageKey ? { ...message, read } : message)));
    setFolderStats((current) => updateUnreadCount(current, context.folder || selectedFolder, context.wasRead, read));
  }

  return {
    folders,
    folderStats,
    selectedFolder,
    messages,
    selectedMessage,
    query,
    setQuery,
    unreadOnly,
    nextCursor,
    moveDialog,
    deleteOpen,
    refreshMailbox,
    loadMessages,
    selectFolder,
    selectMessage,
    toggleRead,
    moveSelected,
    resolvePending,
    openMove() {
      setMoveDialog({ open: true, destination: "", sourceFolder: selectedMessage?.message_ref?.folder || "" });
    },
    closeMove: () => setMoveDialog({ open: false, destination: "", sourceFolder: "" }),
    setMoveDestination: (destination: string) => setMoveDialog((current) => ({ ...current, destination })),
    openDelete: () => setDeleteOpen(true),
    closeDelete: () => setDeleteOpen(false),
    search() {
      const subject = query.trim();
      setAppliedQuery(subject);
      return loadMessages(selectedFolder, { reset: true, subject });
    },
    setUnreadFilter(value: boolean) {
      if (value !== unreadOnly) viewOwner.current = {};
      setUnreadOnly(value);
      if (!busy) void loadMessages(selectedFolder, { reset: true, unread: value });
    },
  };
}

export function mergeMessages(current: MailMessage[], next: MailMessage[]) {
  const merged = new Map(current.map((message) => [messageRefKey(message), message]));
  for (const message of next) merged.set(messageRefKey(message), message);
  return [...merged.values()];
}

export function updateUnreadCount(stats: Record<string, MailFolderStats>, folder: string, wasRead: boolean | undefined, isRead: boolean) {
  if (wasRead === isRead) return stats;
  const current = stats[folder] || { total: 0, unread: 0 };
  return { ...stats, [folder]: { ...current, unread: Math.max(0, current.unread + (isRead ? -1 : 1)) } };
}
