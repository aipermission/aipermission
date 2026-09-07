import { useEffect, useEffectEvent, useState } from "react";
import { connectorActionCode, connectorActionPending } from "../_shared/action-result";
import { messageRefKey } from "./helpers";
import { isStaleMessageFailure } from "./use-mail-action-runner";

export const defaultMailFolder = "INBOX";
const defaultMessageLimit = 50;

export function useMailMailbox({ scopeKey, activeSession, imapEnabled, busy, folderSelectionLocked, runMailAction }) {
  const [folders, setFolders] = useState([]);
  const [folderStats, setFolderStats] = useState({});
  const [selectedFolder, setSelectedFolder] = useState(defaultMailFolder);
  const [messages, setMessages] = useState([]);
  const [selectedMessage, setSelectedMessage] = useState(null);
  const [query, setQuery] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [unreadOnly, setUnreadOnly] = useState(true);
  const [nextCursor, setNextCursor] = useState("");
  const [moveDialog, setMoveDialog] = useState({ open: false, destination: "", sourceFolder: "" });
  const [deleteOpen, setDeleteOpen] = useState(false);
  const refreshForEffect = useEffectEvent((options) => refreshMailbox(options));

  useEffect(() => {
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
  }, [scopeKey]);

  useEffect(() => {
    if (!activeSession.active || !imapEnabled) return;
    void refreshForEffect({ preferredFolder: defaultMailFolder, subject: "" });
  }, [activeSession.active, activeSession.startedAt, scopeKey, imapEnabled]);

  async function refreshMailbox({ preferredFolder = selectedFolder, subject = appliedQuery } = {}) {
    if (!activeSession.active || !imapEnabled) return;
    try {
      const item = await runMailAction("list_folders", {}, "manual Mail workspace folder list", "loading", { preferredFolder, subject });
      if (!item || connectorActionPending(item)) return;
      const preferred = applyFolderResult(item, preferredFolder);
      await loadMessages(preferred, { reset: true, subject });
    } catch {
      // The action runner owns the bounded connector error.
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
      // The action runner owns the bounded connector error.
    }
  }

  async function selectFolder(folder) {
    if (folder === selectedFolder || folderSelectionLocked) return;
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
    }
  }

  async function toggleRead() {
    if (!selectedMessage) return;
    const actionName = selectedMessage.read ? "mark_unread" : "mark_read";
    const context = { messageKey: messageRefKey(selectedMessage), folder: selectedFolder, wasRead: selectedMessage.read };
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

  async function moveSelected(actionName, destination = "") {
    if (!selectedMessage) return;
    const input = { message_ref: selectedMessage.message_ref };
    if (destination) input.destination_folder = destination;
    const context = { messageKey: messageRefKey(selectedMessage), folder: selectedFolder };
    try {
      const item = await runMailAction(actionName, input, `manual Mail workspace ${actionName.replaceAll("_", " ")}`, "updating", context);
      if (!item || connectorActionPending(item)) return;
      applyMoveResult(context);
      await loadMessages(selectedFolder, { reset: true });
    } catch {
      // Confirmation remains available for a deliberate retry.
    }
  }

  async function resolvePending(pending, resolution) {
    const { actionName, context } = pending;
    if (isStaleMessageFailure(resolution)) setSelectedMessage(null);
    if (resolution.state !== "completed") return;
    const { item } = resolution;
    if (actionName === "list_folders") {
      const preferred = applyFolderResult(item, context.preferredFolder);
      await loadMessages(preferred, { reset: true, subject: context.subject });
    } else if (actionName === "search_messages") {
      applyMessageSearchResult(item, context);
    } else if (actionName === "get_message") {
      setSelectedMessage(item.output || null);
    } else if (actionName === "mark_read" || actionName === "mark_unread") {
      applyReadStateResult(item, context);
    } else if (["move_message", "archive_message", "delete_message"].includes(actionName)) {
      applyMoveResult(context);
      await loadMessages(context.folder, { reset: true });
    }
  }

  function applyFolderResult(item, preferredFolder) {
    const nextFolders = Array.isArray(item.output?.folders) ? item.output.folders.filter((folder) => folder.selectable !== false) : [];
    setFolders(nextFolders);
    const preferred = nextFolders.some((folder) => folder.name === preferredFolder)
      ? preferredFolder
      : nextFolders.find((folder) => folder.name.toUpperCase() === defaultMailFolder)?.name || nextFolders[0]?.name || defaultMailFolder;
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
    setMoveDestination: (destination) => setMoveDialog((current) => ({ ...current, destination })),
    openDelete: () => setDeleteOpen(true),
    closeDelete: () => setDeleteOpen(false),
    search() {
      const subject = query.trim();
      setAppliedQuery(subject);
      return loadMessages(selectedFolder, { reset: true, subject });
    },
    setUnreadFilter(value) {
      setUnreadOnly(value);
      if (!busy) void loadMessages(selectedFolder, { reset: true, unread: value });
    },
  };
}

export function mergeMessages(current, next) {
  const merged = new Map(current.map((message) => [messageRefKey(message), message]));
  for (const message of next) merged.set(messageRefKey(message), message);
  return [...merged.values()];
}

export function updateUnreadCount(stats, folder, wasRead, isRead) {
  if (wasRead === isRead) return stats;
  const current = stats[folder] || { total: 0, unread: 0 };
  return { ...stats, [folder]: { ...current, unread: Math.max(0, current.unread + (isRead ? -1 : 1)) } };
}
