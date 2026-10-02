import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";
import { useMailMailbox, updateUnreadCount } from "./use-mailbox";
import { MailActionFailure } from "./use-mail-action-runner";
import type { MailPendingAction, RunMailAction } from "./action-types";
import type { MailMessage } from "./message-types";

const message: MailMessage = { message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, subject: "Status", read: false };
const second: MailMessage = { message_ref: { folder: "INBOX", uidvalidity: 7, uid: 10 }, subject: "Followup", read: false };

function response(action_name: string, output: unknown = {}) {
  return connectorActionFixture({ target_ref: "mail:1:1", connector_kind: "mail", action_name, output });
}

function pending(actionName: string, context: MailPendingAction["context"] = {}): MailPendingAction {
  return { requestID: 1, actionName, context, generation: 1, scope: "mail:1:1" };
}

function mailbox(overrides: Partial<Parameters<typeof useMailMailbox>[0]> = {}) {
  const runMailAction = vi.fn<RunMailAction>().mockImplementation(async (actionName) => {
    if (actionName === "list_folders") return response(actionName, { folders: [{ name: "INBOX" }, { name: "Archive" }] });
    if (actionName === "search_messages") return response(actionName, { messages: [message], total: 2, unread: 1, next_cursor: "page-2" });
    if (actionName === "get_message") return response(actionName, message);
    return response(actionName, { read: actionName === "mark_read" });
  });
  const props: Parameters<typeof useMailMailbox>[0] = {
    scopeKey: "mail:1:1",
    activeSession: { active: true, startedAt: "now" },
    imapEnabled: true,
    busy: false,
    folderSelectionLocked: false,
    runMailAction,
    ...overrides,
  };
  return { runMailAction, props, ...renderHook(useMailMailbox, { initialProps: props }) };
}

it("never loads an inactive or IMAP-disabled mailbox and respects selection locks", async () => {
  const { result, rerender, props, runMailAction } = mailbox({ activeSession: { active: false } });
  await act(async () => result.current.refreshMailbox());
  await act(async () => result.current.loadMessages("INBOX"));
  expect(runMailAction).not.toHaveBeenCalled();
  rerender({ ...props, activeSession: { active: true }, imapEnabled: false, busy: true, folderSelectionLocked: true });
  await act(async () => result.current.refreshMailbox());
  await act(async () => result.current.selectFolder("Archive"));
  await act(async () => result.current.selectMessage(message));
  await act(async () => result.current.toggleRead());
  await act(async () => result.current.moveSelected("move_message", "Archive"));
  expect(runMailAction).not.toHaveBeenCalled();
  expect(result.current.selectedFolder).toBe("INBOX");
  expect(result.current.selectedMessage).toBeNull();
});

it.each([
  [[{ name: "Archive" }, { name: "inbox" }], "inbox"],
  [[{ name: "Archive" }], "Archive"],
  [[], "INBOX"],
])("chooses an available folder when the preferred folder disappeared: %j", async (folders, preferred) => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  runMailAction.mockResolvedValueOnce({ ...response("list_folders"), status: "approval_pending" });
  await act(async () => result.current.refreshMailbox({ preferredFolder: "Missing", subject: "deployment" }));
  const context = runMailAction.mock.calls.at(-1)?.[4];
  expect(context).toBeDefined();
  await act(async () =>
    result.current.resolvePending(
      pending("list_folders", context),
      { state: "completed", item: response("list_folders", { folders }) },
      true,
    ),
  );
  expect(result.current.selectedFolder).toBe(preferred);
  expect(result.current.folders).toHaveLength(folders.length);
  expect(runMailAction).toHaveBeenLastCalledWith(
    "search_messages",
    expect.objectContaining({ folder: preferred, subject: "deployment" }),
    expect.any(String),
    "loading",
    expect.any(Object),
  );
});

it("merges cursor pages by message reference, updates stats and applies trimmed subject searches", async () => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  runMailAction.mockResolvedValueOnce(
    response("search_messages", { messages: [{ ...message, subject: "Updated" }, second], total: 2, unread: 2 }),
  );
  await act(async () => result.current.loadMessages("INBOX", { reset: false, cursor: "page-2" }));
  expect(result.current.messages.map((item) => item.subject)).toEqual(["Updated", "Followup"]);
  expect(result.current.folderStats.INBOX).toEqual({ total: 2, unread: 2 });
  expect(result.current.nextCursor).toBe("");
  expect(runMailAction).toHaveBeenLastCalledWith(
    "search_messages",
    { folder: "INBOX", unread_only: true, subject: "", limit: 50, cursor: "page-2" },
    expect.any(String),
    "loading",
    expect.objectContaining({ reset: false, cursor: "page-2" }),
  );
  act(() => result.current.setQuery("  deployment  "));
  await act(async () => result.current.search());
  expect(runMailAction).toHaveBeenLastCalledWith(
    "search_messages",
    expect.objectContaining({ subject: "deployment", cursor: "" }),
    expect.any(String),
    "loading",
    expect.objectContaining({ reset: true, subject: "deployment" }),
  );
  act(() => result.current.setUnreadFilter(false));
  await waitFor(() => expect(result.current.unreadOnly).toBe(false));
  expect(runMailAction).toHaveBeenLastCalledWith(
    "search_messages",
    expect.objectContaining({ unread_only: false, subject: "deployment" }),
    expect.any(String),
    "loading",
    expect.objectContaining({ unread: false }),
  );
});

it("keeps visible content intact while folder and message actions require approval", async () => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  await act(async () => result.current.selectMessage(message));
  expect(result.current.folders.map((folder) => folder.name)).toEqual(["INBOX", "Archive"]);
  expect(result.current.selectedMessage?.subject).toBe("Status");
  const count = runMailAction.mock.calls.length;
  runMailAction.mockResolvedValueOnce({ ...response("list_folders"), status: "approval_pending" });
  await act(async () => result.current.refreshMailbox());
  expect(runMailAction).toHaveBeenCalledTimes(count + 1);
  expect(result.current.folders.map((folder) => folder.name)).toEqual(["INBOX", "Archive"]);
  expect(result.current.selectedMessage?.subject).toBe("Status");
  runMailAction.mockResolvedValueOnce({ ...response("search_messages"), status: "approval_pending" });
  await act(async () => result.current.loadMessages("INBOX"));
  expect(result.current.messages).toHaveLength(1);
  expect(result.current.selectedMessage?.subject).toBe("Status");
  runMailAction.mockResolvedValueOnce({ ...response("get_message"), status: "approval_pending" });
  await act(async () => result.current.selectMessage(message));
  expect(result.current.selectedMessage?.subject).toBe("Status");
  runMailAction.mockResolvedValueOnce(null);
  await act(async () => result.current.selectMessage(message));
  expect(result.current.selectedMessage?.subject).toBe("Status");
});

it("updates read state in both projections and closes move/delete confirmation only after completion", async () => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  await act(async () => result.current.selectMessage(message));
  await act(async () => result.current.toggleRead());
  expect(result.current.selectedMessage?.read).toBe(true);
  expect(result.current.messages[0].read).toBe(true);
  expect(result.current.folderStats.INBOX.unread).toBe(0);
  await act(async () => result.current.toggleRead());
  expect(result.current.selectedMessage?.read).toBe(false);
  expect(result.current.folderStats.INBOX.unread).toBe(1);
  act(() => {
    result.current.openMove();
    result.current.openDelete();
  });
  act(() => result.current.setMoveDestination("Archive"));
  expect(result.current.moveDialog).toEqual({ open: true, destination: "Archive", sourceFolder: "INBOX" });
  runMailAction.mockResolvedValueOnce({ ...response("move_message"), status: "approval_pending" });
  await act(async () => result.current.moveSelected("move_message", "Archive"));
  expect(result.current.moveDialog.open).toBe(true);
  expect(result.current.deleteOpen).toBe(true);
  runMailAction.mockResolvedValueOnce(response("move_message"));
  await act(async () => result.current.moveSelected("move_message", "Archive"));
  expect(runMailAction.mock.calls.at(-2)?.[1]).toEqual({ message_ref: message.message_ref, destination_folder: "Archive" });
  expect(result.current.selectedMessage).toBeNull();
  expect(result.current.moveDialog.open).toBe(false);
  expect(result.current.deleteOpen).toBe(false);
});

it("does not adjust unread stats for an unchanged flag and never decrements below zero", () => {
  const stats = { INBOX: { total: 3, unread: 0 } };
  expect(updateUnreadCount(stats, "INBOX", true, true)).toBe(stats);
  expect(updateUnreadCount(stats, "INBOX", false, true)).toEqual(stats);
  expect(updateUnreadCount({}, "Archive", true, false)).toEqual({ Archive: { total: 0, unread: 1 } });
});

function delayedResponse() {
  let resolve: (_item: ReturnType<typeof response>) => void = () => {
    throw new Error("Response was not initialized.");
  };
  const promise = new Promise<ReturnType<typeof response>>((finish) => {
    resolve = finish;
  });
  return { promise, resolve };
}

it.each([true, false])("guards search filter/cursor ownership even when transport ignores cancellation (oldFirst=%s)", async (oldFirst) => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  const old = delayedResponse();
  const latest = delayedResponse();
  runMailAction.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
  let oldSearch: Promise<void> | undefined;
  let newSearch: Promise<void> | undefined;
  act(() => {
    oldSearch = result.current.loadMessages("INBOX", { reset: false, cursor: "page-2" });
  });
  act(() => result.current.setQuery("new subject"));
  act(() => {
    newSearch = result.current.search();
  });
  const stale = response("search_messages", { messages: [message], next_cursor: "old-cursor", total: 50, unread: 50 });
  const current = response("search_messages", { messages: [second], next_cursor: "new-cursor", total: 3, unread: 2 });
  if (oldFirst)
    await act(async () => {
      old.resolve(stale);
      await oldSearch;
    });
  await act(async () => {
    latest.resolve(current);
    await newSearch;
  });
  if (!oldFirst)
    await act(async () => {
      old.resolve(stale);
      await oldSearch;
    });
  expect(result.current.messages).toEqual([second]);
  expect(result.current.nextCursor).toBe("new-cursor");
  expect(result.current.folderStats.INBOX).toEqual({ total: 3, unread: 2 });
});

it.each(["list_folders", "search_messages", "get_message"])(
  "rejects an older pending %s completion after a newer folder selection",
  async (actionName) => {
    const { result, runMailAction } = mailbox();
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    runMailAction.mockResolvedValueOnce({ ...response(actionName), status: "approval_pending" });
    await act(async () => {
      if (actionName === "list_folders") await result.current.refreshMailbox();
      else if (actionName === "search_messages") await result.current.loadMessages("INBOX");
      else await result.current.selectMessage(message);
    });
    const context = runMailAction.mock.calls.at(-1)?.[4];
    const archived = { ...second, message_ref: { ...second.message_ref, folder: "Archive" } };
    runMailAction.mockResolvedValueOnce(response("search_messages", { messages: [archived], next_cursor: "archive-cursor" }));
    await act(async () => result.current.selectFolder("Archive"));
    runMailAction.mockResolvedValueOnce(response("get_message", archived));
    await act(async () => result.current.selectMessage(archived));
    const count = runMailAction.mock.calls.length;
    await act(async () =>
      result.current.resolvePending(
        pending(actionName, context),
        {
          state: "completed",
          item: response(
            actionName,
            actionName === "get_message" ? message : { folders: [{ name: "INBOX" }], messages: [message], next_cursor: "old-cursor" },
          ),
        },
        true,
      ),
    );
    expect(result.current.selectedFolder).toBe("Archive");
    expect(result.current.messages).toEqual([archived]);
    expect(result.current.selectedMessage).toEqual(archived);
    expect(result.current.nextCursor).toBe("archive-cursor");
    expect(runMailAction).toHaveBeenCalledTimes(count);
  },
);

it.each(["search_messages", "get_message"])("applies the still-owned pending %s completion", async (actionName) => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  runMailAction.mockResolvedValueOnce({ ...response(actionName), status: "approval_pending" });
  await act(async () => (actionName === "get_message" ? result.current.selectMessage(second) : result.current.loadMessages("INBOX")));
  const context = runMailAction.mock.calls.at(-1)?.[4];
  await act(async () =>
    result.current.resolvePending(
      pending(actionName, context),
      { state: "completed", item: response(actionName, actionName === "get_message" ? second : { messages: [second] }) },
      true,
    ),
  );
  expect(result.current.messages).toEqual(actionName === "get_message" ? [message] : [second]);
  expect(result.current.selectedMessage).toEqual(actionName === "get_message" ? second : null);
});

it("keeps a different selected message when an immediate read fails with a stale reference", async () => {
  const { result, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  await act(async () => result.current.selectMessage(message));
  const failure = new MailActionFailure("Stale reference", response("get_message", { code: "stale_message_reference" }), {
    actionName: "get_message",
    summary: "Stale reference",
    item: null,
  });
  runMailAction.mockRejectedValueOnce(failure);
  await act(async () => result.current.selectMessage(second));
  expect(result.current.selectedMessage).toEqual(message);
  runMailAction.mockRejectedValueOnce(failure);
  await act(async () => result.current.selectMessage(message));
  expect(result.current.selectedMessage).toBeNull();
});

it("does not let an immediate mutation completion reset a filter changed while busy", async () => {
  const { result, rerender, props, runMailAction } = mailbox();
  await waitFor(() => expect(result.current.messages).toHaveLength(1));
  await act(async () => result.current.selectMessage(message));
  const mutation = delayedResponse();
  runMailAction.mockReturnValueOnce(mutation.promise);
  let moving: Promise<void> | undefined;
  act(() => {
    moving = result.current.moveSelected("delete_message");
  });
  rerender({ ...props, busy: true });
  act(() => result.current.setUnreadFilter(false));
  const count = runMailAction.mock.calls.length;
  await act(async () => {
    mutation.resolve(response("delete_message"));
    await moving;
  });
  expect(result.current.unreadOnly).toBe(false);
  expect(result.current.messages).toEqual([]);
  expect(result.current.selectedMessage).toBeNull();
  expect(result.current.nextCursor).toBe("page-2");
  expect(runMailAction).toHaveBeenCalledTimes(count);
  expect(result.current.folderStats.INBOX).toBeUndefined();
});
