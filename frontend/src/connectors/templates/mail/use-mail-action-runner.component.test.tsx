import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { MailActionFailure, useMailActionRunner } from "./use-mail-action-runner";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";
import { APIError } from "../../../lib/errors";
import { useMailWorkspace } from "./use-mail-workspace";
import type { MailWorkspaceProps } from "./use-mail-workspace";
import type { MailActionItem } from "./action-types";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

function deferred<T = void>() {
  let resolve: (_value: T) => void = () => {
    throw new Error("Deferred promise was not initialized.");
  };
  let reject: (_error: Error) => void = () => {
    throw new Error("Deferred promise was not initialized.");
  };
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.mocked(apiPost).mockReset();
});

it("does not let an old activity refresh failure replace a newer target result", async () => {
  const oldRefresh = deferred();
  const refreshes = [() => oldRefresh.promise, () => Promise.resolve()];
  const props = {
    target: { ref: "mail:1:1" },
    approvals: { data: [] },
    scopeKey: "mail:1:1:session-a",
    onRefreshActivity: vi.fn(() => refreshes.shift()?.()),
    onResolution: vi.fn(),
  };
  vi.mocked(apiPost).mockImplementation(async (_path, payload) =>
    connectorActionFixture({
      target_ref: connectorActionRequest(payload).target_ref,
      action_name: connectorActionRequest(payload).action_name,
      output: {},
    }),
  );
  const { result, rerender } = renderHook((value) => useMailActionRunner(value), { initialProps: props });

  await act(async () => result.current.runMailAction("send_message", {}, "first send"));
  rerender({ ...props, target: { ref: "mail:2:2" }, scopeKey: "mail:2:2:session-b" });
  await act(async () => result.current.runMailAction("send_message", {}, "second send"));
  await act(async () => oldRefresh.reject(new Error("old refresh failed")));

  expect(result.current.state).toMatchObject({
    state: "idle",
    error: "",
    message: "Message accepted for SMTP delivery.",
  });
});

it("rejects wrong-target and malformed action envelopes before reporting completion", async () => {
  const props = { target: { ref: "mail:1:1" }, approvals: { data: [] }, scopeKey: "mail:1:1:active", onRefreshActivity: vi.fn() };
  const { result } = renderHook(() => useMailActionRunner(props));
  vi.mocked(apiPost).mockResolvedValue(connectorActionFixture({ target_ref: "mail:2:2", action_name: "get_message" }));
  await act(async () => {
    await expect(result.current.runMailAction("get_message", {}, "read")).rejects.toThrow("Invalid connector action response");
  });
  expect(result.current.state).toMatchObject({ state: "error", message: "" });
  expect(props.onRefreshActivity).not.toHaveBeenCalled();
  vi.mocked(apiPost).mockResolvedValue({ status: "completed", output: { subject: "untrusted" } });
  await act(async () => {
    await expect(result.current.runMailAction("get_message", {}, "read")).rejects.toThrow("Invalid connector action response");
  });
  expect(result.current.state.result?.item).toBeNull();
});

it("discards a delayed response after the same target starts a different session", async () => {
  const pending = deferred<ReturnType<typeof connectorActionFixture>>();
  vi.mocked(apiPost).mockReturnValue(pending.promise);
  const props = { target: { ref: "mail:1:1" }, scopeKey: "session-a", onRefreshActivity: vi.fn(), onResolution: vi.fn() };
  const { result, rerender } = renderHook((value) => useMailActionRunner(value), { initialProps: props });
  let request: ReturnType<typeof result.current.runMailAction> | undefined;
  act(() => {
    request = result.current.runMailAction("get_message", {}, "read");
  });
  rerender({ ...props, scopeKey: "session-b" });
  await act(async () => {
    pending.resolve(connectorActionFixture({ target_ref: props.target.ref, action_name: "get_message" }));
    expect(await request).toBeNull();
  });
  expect(result.current.state).toEqual({ state: "idle", error: "", message: "" });
  expect(props.onRefreshActivity).not.toHaveBeenCalled();
  expect(props.onResolution).not.toHaveBeenCalled();
  expect(result.current.resultDialog.open).toBe(false);
});

it("ignores a delayed rejection after unmount without refreshing activity", async () => {
  const pending = deferred<ReturnType<typeof connectorActionFixture>>();
  vi.mocked(apiPost).mockReturnValue(pending.promise);
  const onRefreshActivity = vi.fn();
  const { result, unmount } = renderHook(() =>
    useMailActionRunner({ target: { ref: "mail:1:1" }, scopeKey: "session-a", onRefreshActivity }),
  );
  let request: ReturnType<typeof result.current.runMailAction> | undefined;
  act(() => {
    request = result.current.runMailAction("get_message", {}, "read");
  });
  unmount();
  await act(async () => {
    pending.reject(new Error("delayed failure"));
    expect(await request).toBeNull();
  });
  expect(onRefreshActivity).not.toHaveBeenCalled();
});

it("preserves unknown SMTP metadata and the original transport error identity", async () => {
  const response = connectorActionFixture({
    target_ref: "mail:1:1",
    action_name: "send_message",
    status: "outcome_unknown",
    error: "SMTP result unknown",
    output: { submission_status: "submission_unknown", message_id: "test-message" },
  });
  vi.mocked(apiPost).mockResolvedValue(response);
  const { result } = renderHook(() => useMailActionRunner({ target: { ref: "mail:1:1" }, scopeKey: "session-a" }));
  await act(async () => {
    await expect(result.current.runMailAction("send_message", {}, "send")).rejects.toMatchObject({
      actionItem: response,
      actionResult: { item: response },
    });
  });
  expect(result.current.resultDialog).toMatchObject({ open: true, item: response });
  expect(result.current.state).toMatchObject({ state: "error", error: "SMTP result unknown" });
  const failure = new APIError("Transport rejected", { status: 409, code: "test-error", data: { detail: "preserved" } });
  vi.mocked(apiPost).mockRejectedValue(failure);
  await act(async () => {
    await expect(result.current.runMailAction("send_message", {}, "send")).rejects.toBe(failure);
  });
  expect(failure).not.toBeInstanceOf(MailActionFailure);
  expect(failure).toMatchObject({ status: 409, code: "test-error", data: { detail: "preserved" } });
});

const inboxMessage = { message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, subject: "Inbox", read: false };
const archiveMessage = { ...inboxMessage, message_ref: { ...inboxMessage.message_ref, folder: "Archive" }, subject: "Archive" };

function mailResponse(actionName: string, output: unknown = {}) {
  return connectorActionFixture({ target_ref: "mail:1:1", connector_kind: "mail", action_name: actionName, output });
}

function searchResponse(folder: string, messages = [folder === "INBOX" ? inboxMessage : archiveMessage]) {
  return mailResponse("search_messages", {
    folder,
    messages,
    count: messages.length,
    total: 2,
    unread: 1,
    next_cursor: `${folder}-page-2`,
  });
}

async function pendingMutation(actionName: string, status: MailActionItem["status"] = "running", messages = [inboxMessage]) {
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => {
    const request = connectorActionRequest(payload);
    if (request.action_name === "list_folders")
      return mailResponse(request.action_name, { folders: [{ name: "INBOX" }, { name: "Archive" }] });
    if (request.action_name === "search_messages")
      return searchResponse(String(request.input.folder), request.input.folder === "INBOX" ? messages : [archiveMessage]);
    if (request.action_name === "get_message") {
      return mailResponse(request.action_name, { ...inboxMessage, message_ref: request.input.message_ref });
    }
    return { ...mailResponse(request.action_name), request_id: 901, status };
  });
  const props: MailWorkspaceProps = { target: { ref: "mail:1:1" }, session: { active: true, startedAt: "now" }, approvals: { data: [] } };
  const hook = renderHook(useMailWorkspace, { initialProps: props });
  await waitFor(() => expect(hook.result.current.mailbox.messages).toEqual(messages));
  vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", inboxMessage));
  await act(async () => hook.result.current.mailbox.selectMessage(inboxMessage));
  await act(async () =>
    actionName === "mark_read"
      ? hook.result.current.mailbox.toggleRead()
      : hook.result.current.mailbox.moveSelected(actionName, actionName === "move_message" ? "Archive" : ""),
  );
  expect(hook.result.current.runner.pendingActions[901]?.actionName).toBe(actionName);
  function settle(status: MailActionItem["status"] = "completed", code?: string, output: unknown = {}) {
    hook.rerender({
      ...props,
      approvals: { data: [{ ...mailResponse(actionName, code ? { code } : output), request_id: 901, id: 901, status }] },
    });
  }
  return { ...hook, props, settle };
}

it.each(
  ["delete_message", "move_message", "archive_message"].flatMap((actionName) =>
    (["running", "approval_pending"] as const).flatMap((status) =>
      [true, false].map((searchFirst) => ({ actionName, status, searchFirst })),
    ),
  ),
)("keeps Archive ownership for $actionName ($status, searchFirst=$searchFirst)", async ({ actionName, status, searchFirst }) => {
  const { result, settle } = await pendingMutation(actionName, status);
  const search = deferred<ReturnType<typeof searchResponse>>();
  vi.mocked(apiPost).mockReturnValueOnce(search.promise);
  let selecting: Promise<void> | undefined;
  act(() => {
    selecting = result.current.mailbox.selectFolder("Archive");
  });
  const signal = vi.mocked(apiPost).mock.calls.at(-1)?.[2]?.signal;
  if (searchFirst) {
    await act(async () => {
      search.resolve(searchResponse("Archive"));
      await selecting;
    });
    vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", archiveMessage));
    await act(async () => result.current.mailbox.selectMessage(archiveMessage));
    act(() => {
      result.current.mailbox.openMove();
      result.current.mailbox.openDelete();
    });
  }
  const state = result.current.runner.state;
  const searches = vi.mocked(apiPost).mock.calls.length;
  settle();
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(signal?.aborted).toBe(false);
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(searches);
  expect(result.current.runner.state).toEqual(state);
  if (!searchFirst)
    await act(async () => {
      search.resolve(searchResponse("Archive"));
      await selecting;
    });
  expect(result.current.mailbox.selectedFolder).toBe("Archive");
  expect(result.current.mailbox.messages).toEqual([archiveMessage]);
  expect(result.current.mailbox.nextCursor).toBe("Archive-page-2");
  if (searchFirst) {
    expect(result.current.mailbox.selectedMessage).toEqual(archiveMessage);
    expect(result.current.mailbox.moveDialog.open).toBe(true);
    expect(result.current.mailbox.deleteOpen).toBe(true);
  }
  expect(result.current.mailbox.folderStats.INBOX).toBeUndefined();
});

it.each(
  ["delete_message", "move_message", "archive_message"].flatMap((actionName) =>
    ["search_messages", "get_message"].flatMap((browserAction) =>
      [false, true].flatMap((pendingBrowser) =>
        (["completed", "failed", "outcome_unknown"] as const).map((status) => ({ actionName, browserAction, pendingBrowser, status })),
      ),
    ),
  ),
)(
  "reconciles delayed $browserAction after $actionName $status (pending=$pendingBrowser)",
  async ({ actionName, browserAction, pendingBrowser, status }) => {
    const next = { ...inboxMessage, message_ref: { ...inboxMessage.message_ref, uid: 10 }, subject: "Next" };
    const { result, settle, rerender, props } = await pendingMutation(actionName, "running", [inboxMessage, next]);
    if (browserAction === "get_message") {
      vi.mocked(apiPost).mockResolvedValueOnce(mailResponse(browserAction, next));
      await act(async () => result.current.mailbox.selectMessage(next));
    }
    const browser = deferred<ReturnType<typeof mailResponse>>();
    vi.mocked(apiPost).mockReturnValueOnce(browser.promise);
    let reading: Promise<void> | undefined;
    act(() => {
      reading =
        browserAction === "get_message" ? result.current.mailbox.selectMessage(inboxMessage) : result.current.mailbox.loadMessages("INBOX");
      result.current.mailbox.openMove();
      result.current.mailbox.setMoveDestination("Archive");
      result.current.mailbox.openDelete();
    });
    const signal = vi.mocked(apiPost).mock.calls.at(-1)?.[2]?.signal;
    const state = result.current.runner.state;
    const calls = vi.mocked(apiPost).mock.calls.length;
    settle(status);
    await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
    expect(result.current.mailbox.messages).toEqual(status === "completed" ? [next] : [inboxMessage, next]);
    expect(result.current.mailbox.selectedMessage).toEqual(
      browserAction === "get_message" ? next : status === "completed" ? null : inboxMessage,
    );
    expect(result.current.runner.state).toEqual(state);
    expect(signal?.aborted).toBe(false);
    expect(vi.mocked(apiPost).mock.calls).toHaveLength(calls);
    const snapshot =
      browserAction === "get_message" ? mailResponse(browserAction, inboxMessage) : searchResponse("INBOX", [inboxMessage, next]);
    await act(async () => {
      browser.resolve(pendingBrowser ? { ...snapshot, request_id: 902, status: "running" } : snapshot);
      await reading;
    });
    if (pendingBrowser) {
      expect(result.current.runner.pendingActions[902]?.actionName).toBe(browserAction);
      rerender({ ...props, approvals: { data: [{ ...snapshot, request_id: 902, id: 902 }] } });
      await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
    }
    expect(result.current.mailbox.messages).toEqual(status === "completed" ? [next] : [inboxMessage, next]);
    expect(result.current.mailbox.selectedMessage).toEqual(
      browserAction === "get_message" ? (status === "completed" ? next : inboxMessage) : null,
    );
    expect(result.current.mailbox.nextCursor).toBe("INBOX-page-2");
    expect(result.current.mailbox.moveDialog).toEqual({ open: true, destination: "Archive", sourceFolder: "INBOX" });
    expect(result.current.mailbox.deleteOpen).toBe(true);
    expect(result.current.runner.state).toMatchObject({ state: "idle", error: "", result: { actionName: browserAction } });
  },
);

it.each(["uid", "uidvalidity"] as const)("allows a fresh %s after exact-reference retirement", async (field) => {
  const { result, settle } = await pendingMutation("delete_message");
  await act(async () => result.current.mailbox.loadMessages("INBOX"));
  settle();
  await waitFor(() => expect(result.current.mailbox.messages).toEqual([]));
  const fresh = { ...inboxMessage, message_ref: { ...inboxMessage.message_ref, [field]: inboxMessage.message_ref[field] + 1 } };
  vi.mocked(apiPost).mockResolvedValueOnce(searchResponse("INBOX", [fresh]));
  await act(async () => result.current.mailbox.loadMessages("INBOX"));
  expect(result.current.mailbox.messages).toEqual([fresh]);
  vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", fresh));
  await act(async () => result.current.mailbox.selectMessage(fresh));
  expect(result.current.mailbox.selectedMessage).toEqual(fresh);
});

it.each(["target", "session"])("does not carry retired references into a different %s", async (scope) => {
  const { result, settle, rerender, props } = await pendingMutation("delete_message");
  await act(async () => result.current.mailbox.loadMessages("INBOX"));
  settle();
  await waitFor(() => expect(result.current.mailbox.messages).toEqual([]));
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => {
    const request = connectorActionRequest(payload);
    const response =
      request.action_name === "list_folders"
        ? mailResponse(request.action_name, { folders: [{ name: "INBOX" }] })
        : request.action_name === "search_messages"
          ? searchResponse("INBOX")
          : mailResponse(request.action_name, inboxMessage);
    return { ...response, target_ref: request.target_ref };
  });
  rerender({
    ...props,
    target: scope === "target" ? { ref: "mail:2:2" } : props.target,
    session: scope === "session" ? { active: true, startedAt: "later" } : props.session,
    approvals: { data: [] },
  });
  await waitFor(() => expect(result.current.mailbox.messages).toEqual([inboxMessage]));
  await act(async () => result.current.mailbox.selectMessage(inboxMessage));
  expect(result.current.mailbox.selectedMessage).toEqual(inboxMessage);
});

it.each(["failed", "declined", "canceled", "outcome_unknown"] as const)(
  "settles an offscreen %s mutation without replacing the current error or selection",
  async (status) => {
    const { result, settle } = await pendingMutation("delete_message");
    await act(async () => result.current.mailbox.selectFolder("Archive"));
    vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", archiveMessage));
    await act(async () => result.current.mailbox.selectMessage(archiveMessage));
    vi.mocked(apiPost).mockRejectedValueOnce(new Error("Current search failed"));
    await act(async () => result.current.mailbox.loadMessages("Archive"));
    const state = result.current.runner.state;
    settle(status, "stale_message_reference");
    await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
    expect(result.current.mailbox.messages).toEqual([archiveMessage]);
    expect(result.current.mailbox.selectedMessage).toEqual(archiveMessage);
    expect(result.current.runner.state).toEqual(state);
  },
);

it.each(["delete_message", "move_message", "archive_message"])(
  "reloads an unchanged source view after pending %s completes",
  async (actionName) => {
    const { result, settle } = await pendingMutation(actionName);
    act(() => {
      result.current.mailbox.openMove();
      result.current.mailbox.openDelete();
    });
    const count = vi.mocked(apiPost).mock.calls.length;
    settle();
    await waitFor(() => expect(vi.mocked(apiPost).mock.calls).toHaveLength(count + 1));
    await waitFor(() => expect(result.current.runner.state.state).toBe("idle"));
    expect(connectorActionRequest(vi.mocked(apiPost).mock.calls.at(-1)?.[1])).toMatchObject({
      action_name: "search_messages",
      input: { folder: "INBOX", cursor: "" },
    });
    expect(result.current.mailbox.selectedMessage).toBeNull();
    expect(result.current.mailbox.moveDialog.open).toBe(false);
    expect(result.current.mailbox.deleteOpen).toBe(false);
    expect(result.current.mailbox.folderStats.INBOX).toEqual({ total: 2, unread: 1 });
  },
);

it.each(["subject", "unread", "cursor", "selection"])("does not reset a newer same-folder %s after mutation completion", async (change) => {
  const next = { ...inboxMessage, message_ref: { ...inboxMessage.message_ref, uid: 10 }, subject: "Next" };
  const { result, settle } = await pendingMutation("delete_message", "running", [inboxMessage, next]);
  if (change === "subject") {
    act(() => result.current.mailbox.setQuery("new filter"));
    await act(async () => result.current.mailbox.search());
  } else if (change === "unread") {
    await act(async () => result.current.mailbox.setUnreadFilter(false));
  } else if (change === "cursor") {
    await act(async () => result.current.mailbox.loadMessages("INBOX", { reset: false, cursor: "INBOX-page-2" }));
  } else {
    vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", next));
    await act(async () => result.current.mailbox.selectMessage(next));
    act(() => {
      result.current.mailbox.openMove();
      result.current.mailbox.setMoveDestination("Archive");
      result.current.mailbox.openDelete();
    });
  }
  const before = {
    selectedFolder: result.current.mailbox.selectedFolder,
    query: result.current.mailbox.query,
    unreadOnly: result.current.mailbox.unreadOnly,
    nextCursor: result.current.mailbox.nextCursor,
    moveDialog: result.current.mailbox.moveDialog,
    deleteOpen: result.current.mailbox.deleteOpen,
  };
  const state = result.current.runner.state;
  const count = vi.mocked(apiPost).mock.calls.length;
  settle();
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
  expect(result.current.mailbox).toMatchObject(before);
  expect(result.current.mailbox.messages).toEqual([next]);
  expect(result.current.mailbox.selectedMessage).toEqual(change === "selection" ? next : null);
  expect(result.current.runner.state).toEqual(state);
});

it.each(
  ["delete_message", "move_message", "archive_message"].flatMap((actionName) =>
    (["failed", "outcome_unknown"] as const).map((status) => ({ actionName, status })),
  ),
)("retains the exact source reference after $actionName resolves $status", async ({ actionName, status }) => {
  const { result, settle } = await pendingMutation(actionName);
  const count = vi.mocked(apiPost).mock.calls.length;
  settle(status);
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(result.current.mailbox.messages).toEqual([inboxMessage]);
  expect(result.current.mailbox.selectedMessage).toEqual(inboxMessage);
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
});

it("reconciles offscreen read state by exact reference without resetting Archive", async () => {
  const { result, settle } = await pendingMutation("mark_read");
  await act(async () => result.current.mailbox.selectFolder("Archive"));
  vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", archiveMessage));
  await act(async () => result.current.mailbox.selectMessage(archiveMessage));
  const count = vi.mocked(apiPost).mock.calls.length;
  settle("completed", undefined, { read: true });
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(result.current.mailbox.folderStats.INBOX.unread).toBe(0);
  expect(result.current.mailbox.messages).toEqual([archiveMessage]);
  expect(result.current.mailbox.selectedMessage).toEqual(archiveMessage);
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
});

it("drops only the still-owned selection on a pending stale-reference failure", async () => {
  const { result, settle } = await pendingMutation("delete_message");
  settle("failed", "stale_message_reference");
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(result.current.mailbox.selectedMessage).toBeNull();
  expect(result.current.mailbox.messages).toEqual([inboxMessage]);
  expect(result.current.runner.state).toMatchObject({ state: "error", result: { actionName: "delete_message" } });
});

it.each(["target", "session"])("discards pending reconciliation after the %s scope changes", async (change) => {
  const { result, rerender, props } = await pendingMutation("delete_message");
  const count = vi.mocked(apiPost).mock.calls.length;
  rerender({ ...props, target: { ref: change === "target" ? "mail:2:2" : "mail:1:1" }, session: { active: false, startedAt: "later" } });
  rerender({
    ...props,
    target: { ref: change === "target" ? "mail:2:2" : "mail:1:1" },
    session: { active: false, startedAt: "later" },
    approvals: { data: [{ ...mailResponse("delete_message"), id: 901 }] },
  });
  await act(async () => {});
  expect(result.current.runner.pendingActions).toEqual({});
  expect(result.current.runner.state).toEqual({ state: "idle", error: "", message: "" });
  expect(result.current.mailbox.messages).toEqual([]);
  expect(result.current.mailbox.folderStats).toEqual({});
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
});

it("does not let an older mutation reload cancel a newer same-folder write", async () => {
  const { result, settle } = await pendingMutation("delete_message");
  const write = deferred<ReturnType<typeof mailResponse>>();
  vi.mocked(apiPost).mockReturnValueOnce(write.promise);
  let updating: Promise<void> | undefined;
  act(() => {
    updating = result.current.mailbox.toggleRead();
  });
  const signal = vi.mocked(apiPost).mock.calls.at(-1)?.[2]?.signal;
  const count = vi.mocked(apiPost).mock.calls.length;
  settle();
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(signal?.aborted).toBe(false);
  expect(result.current.runner.state.state).toBe("updating");
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
  expect(result.current.mailbox.messages).toEqual([]);
  expect(result.current.mailbox.selectedMessage).toBeNull();
  await act(async () => {
    write.resolve(mailResponse("mark_read", { read: true }));
    await updating;
  });
  expect(result.current.mailbox.messages).toEqual([]);
  expect(result.current.mailbox.selectedMessage).toBeNull();
  expect(signal?.aborted).toBe(false);
});

it("removes an acknowledged reference after folder ABA without regaining dialog or reload ownership", async () => {
  const { result, settle } = await pendingMutation("delete_message");
  await act(async () => result.current.mailbox.selectFolder("Archive"));
  await act(async () => result.current.mailbox.selectFolder("INBOX"));
  vi.mocked(apiPost).mockResolvedValueOnce(mailResponse("get_message", inboxMessage));
  await act(async () => result.current.mailbox.selectMessage(inboxMessage));
  act(() => result.current.mailbox.openDelete());
  const state = result.current.runner.state;
  const count = vi.mocked(apiPost).mock.calls.length;
  settle();
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
  expect(result.current.mailbox.messages).toEqual([]);
  expect(result.current.mailbox.selectedMessage).toBeNull();
  expect(result.current.mailbox.nextCursor).toBe("INBOX-page-2");
  expect(result.current.mailbox.deleteOpen).toBe(true);
  expect(result.current.runner.state).toEqual(state);
});

const outboundFields = { to: ["receiver@example.test"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };

it.each(
  ["delete_message", "move_message", "archive_message"].flatMap((mutation) =>
    ["send_message", "reply_message"].flatMap((outboundAction) =>
      [true, false].flatMap((outboundFirst) =>
        (["completed", "outcome_unknown"] as const).map((outcome) => ({ mutation, outboundAction, outboundFirst, outcome })),
      ),
    ),
  ),
)(
  "preserves newer $outboundAction visibility after $mutation ($outcome, outboundFirst=$outboundFirst)",
  async ({ mutation, outboundAction, outboundFirst, outcome }) => {
    const { result, settle } = await pendingMutation(mutation);
    act(() => (outboundAction === "reply_message" ? result.current.openReply() : result.current.openCompose()));
    const outbound = deferred<ReturnType<typeof mailResponse>>();
    vi.mocked(apiPost).mockReturnValueOnce(outbound.promise);
    let sending: Promise<void> | undefined;
    act(() => {
      sending = result.current.compose.submitMessage(outboundFields);
    });
    const signal = vi.mocked(apiPost).mock.calls.at(-1)?.[2]?.signal;
    expect(signal).toBeDefined();
    expect(connectorActionRequest(vi.mocked(apiPost).mock.calls.at(-1)?.[1]).action_name).toBe(outboundAction);
    const unknown = outcome === "outcome_unknown";
    const response = {
      ...mailResponse(outboundAction, unknown ? { submission_status: "submission_unknown", message_id: "unknown-submission" } : {}),
      status: outcome,
      error: unknown ? "SMTP result unknown" : "",
    };
    async function finishOutbound() {
      await act(async () => {
        outbound.resolve(response);
        await sending;
      });
    }
    if (outboundFirst) await finishOutbound();
    const state = result.current.runner.state;
    const count = vi.mocked(apiPost).mock.calls.length;
    settle();
    await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
    const stateAfterMutation = result.current.runner.state;
    if (!outboundFirst) await finishOutbound();

    expect(signal?.aborted).toBe(false);
    expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
    expect(stateAfterMutation).toEqual(state);
    expect(result.current.mailbox.messages).toEqual([]);
    expect(result.current.mailbox.selectedMessage).toBeNull();
    expect(result.current.mailbox.nextCursor).toBe("INBOX-page-2");
    expect(result.current.mailbox.folderStats.INBOX).toBeUndefined();
    expect(result.current.runner.state).toMatchObject({
      state: unknown ? "error" : "idle",
      error: unknown ? "SMTP result unknown" : "",
      result: { actionName: outboundAction, item: response },
    });
    if (!unknown) {
      expect(result.current.compose.compose.open).toBe(false);
      return;
    }
    expect(result.current.runner.resultDialog).toMatchObject({ open: true, actionName: outboundAction, item: response });
    expect(result.current.compose.compose).toMatchObject({
      open: true,
      reply: outboundAction === "reply_message",
      submissionUnknown: { messageID: "unknown-submission" },
    });
    await act(async () => result.current.compose.submitMessage(outboundFields));
    expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
    expect(result.current.compose.retryDialog).toMatchObject({ open: true, draftChanged: false, messageID: "unknown-submission" });
    const changedFields = { ...outboundFields, subject: "Changed" };
    await act(async () => result.current.compose.submitMessage(changedFields));
    expect(vi.mocked(apiPost).mock.calls).toHaveLength(count);
    expect(result.current.compose.retryDialog).toMatchObject({ open: true, draftChanged: true, messageID: "unknown-submission" });
    vi.mocked(apiPost).mockResolvedValueOnce(mailResponse(outboundAction));
    await act(async () => result.current.compose.confirmRetry());
    expect(vi.mocked(apiPost).mock.calls).toHaveLength(count + 1);
    expect(connectorActionRequest(vi.mocked(apiPost).mock.calls.at(-1)?.[1])).toMatchObject({
      action_name: outboundAction,
      input: changedFields,
    });
    expect(result.current.compose.compose.open).toBe(false);
    expect(result.current.compose.retryDialog.open).toBe(false);
  },
);
