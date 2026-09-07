import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useMailWorkspace } from "./use-mail-workspace";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

const target = {
  ref: "mail:1:1",
  public: {
    imap_enabled: true,
    smtp_auth_mode: "reuse_imap",
    allowed_mutation_destination_folders: ["Archive", "Trash"],
    archive_folder: "Archive",
    trash_folder: "Trash",
  },
};
const message = {
  message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 },
  subject: "Status",
  from: [{ address: "sender@example.com" }],
  read: false,
};

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, payload) => actionResponse(payload.action_name, payload.input));
});

function renderWorkspace(overrides = {}) {
  const props = {
    target,
    approvals: { data: [] },
    session: { active: true, startedAt: "now" },
    onRefreshActivity: vi.fn(),
    ...overrides,
  };
  return { ...renderHook((next) => useMailWorkspace(next), { initialProps: props }), props };
}

it("loads the Mail workspace and keeps read state coherent", async () => {
  const { result } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  expect(result.current.mailbox.folders.map((folder) => folder.name)).toEqual(["INBOX", "Archive"]);

  await act(async () => result.current.mailbox.selectMessage(message));
  expect(result.current.mailbox.selectedMessage?.subject).toBe("Status");
  await act(async () => result.current.mailbox.toggleRead());

  expect(result.current.mailbox.selectedMessage?.read).toBe(true);
  expect(result.current.mailbox.messages[0].read).toBe(true);
  expect(result.current.mailbox.folderStats.INBOX.unread).toBe(0);
});

it("reconciles an approved outbound action without losing its draft early", async () => {
  apiPost.mockImplementation(async (_path, payload) => {
    if (payload.action_name === "send_message") return { id: 41, status: "approval_pending", display_text: "Awaiting approval" };
    return actionResponse(payload.action_name, payload.input);
  });
  const { result, rerender, props } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => result.current.openCompose());
  const fields = { to: ["one@example.com"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };
  await act(async () => result.current.compose.submitMessage(fields));

  expect(result.current.compose.compose).toMatchObject({ open: true, pendingRequestID: 41, form: fields });
  rerender({
    ...props,
    approvals: { data: [{ id: 41, target_ref: target.ref, action_name: "send_message", status: "completed", output: {} }] },
  });
  await waitFor(() => expect(result.current.compose.compose.open).toBe(false));
  expect(result.current.runner.state.message).toBe("Message accepted for SMTP delivery.");
});

it("ignores a Mail response that completes after the target scope changes", async () => {
  let resolveFolders;
  apiPost.mockImplementation(
    (_path, payload) =>
      payload.action_name === "list_folders"
        ? new Promise((resolve) => {
            resolveFolders = resolve;
          })
        : Promise.resolve(actionResponse(payload.action_name, payload.input)),
  );
  const { result, rerender, props } = renderWorkspace();
  await waitFor(() => expect(resolveFolders).toBeTypeOf("function"));
  rerender({ ...props, target: { ...target, ref: "mail:2:2" }, session: { active: false, startedAt: "" } });

  await act(async () => resolveFolders(actionResponse("list_folders", {})));
  expect(result.current.mailbox.folders).toEqual([]);
  expect(apiPost.mock.calls.filter(([, payload]) => payload.action_name === "search_messages")).toHaveLength(0);
});

it("allows a newer folder selection to supersede an in-flight message read", async () => {
  let resolveMessage;
  apiPost.mockImplementation(async (_path, payload) => {
    if (payload.action_name === "get_message") {
      return new Promise((resolve) => {
        resolveMessage = resolve;
      });
    }
    return actionResponse(payload.action_name, payload.input);
  });
  const { result } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => {
    void result.current.mailbox.selectMessage(message);
  });
  await waitFor(() => expect(resolveMessage).toBeTypeOf("function"));
  await act(async () => result.current.mailbox.selectFolder("Archive"));
  await act(async () => resolveMessage(actionResponse("get_message")));

  expect(result.current.mailbox.selectedFolder).toBe("Archive");
  expect(result.current.mailbox.selectedMessage).toBeNull();
});

function actionResponse(actionName) {
  const outputs = {
    list_folders: { folders: [{ name: "INBOX" }, { name: "Archive" }], count: 2 },
    search_messages: { folder: "INBOX", messages: [message], count: 1, total: 1, unread: 1 },
    get_message: message,
    mark_read: { read: true },
  };
  return { id: 1, status: "completed", action_name: actionName, output: outputs[actionName] || {} };
}
