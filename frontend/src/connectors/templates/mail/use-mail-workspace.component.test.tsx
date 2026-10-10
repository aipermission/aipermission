import { connectorActionRequest, connectorActionFixture } from "../../../test/connector-action-fixtures";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useMailWorkspace } from "./use-mail-workspace";
import type { MailWorkspaceProps } from "./use-mail-workspace";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));
vi.mock("../_shared/use-connector-mutation-ownership", () => ({
  useConnectorMutationOwnership: () => ({
    locked: false,
    run: (operation: (_onPending: () => void) => Promise<unknown>) => operation(() => {}),
  }),
}));

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

const post = vi.mocked(apiPost);
beforeEach(() => {
  post.mockReset();
  post.mockImplementation(async (_path, payload) => actionResponse(connectorActionRequest(payload).action_name));
});

function renderWorkspace(overrides: Partial<MailWorkspaceProps> = {}) {
  const props: MailWorkspaceProps = {
    target,
    approvals: { data: [] },
    session: { active: true, startedAt: "now" },
    onRefreshActivity: vi.fn(),
    ...overrides,
  };
  return { ...renderHook((next) => useMailWorkspace(next), { initialProps: props }), props };
}

function mockOutbound(response: Partial<ReturnType<typeof connectorActionFixture>>) {
  post.mockImplementation(async (_path, payload) => {
    const action = connectorActionRequest(payload).action_name;
    return actionResponse(action, action === "send_message" || action === "reply_message" ? response : {});
  });
}

function outboundApproval(overrides: Partial<ReturnType<typeof connectorActionFixture>>) {
  return { id: 41, ...actionResponse("send_message", { request_id: 41, ...overrides }) };
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
  mockOutbound({ request_id: 41, status: "approval_pending", display_text: "Awaiting approval" });
  const { result, rerender, props } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => result.current.openCompose());
  const fields = { to: ["one@example.com"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };
  await act(async () => result.current.compose.submitMessage(fields));

  expect(result.current.compose.compose).toMatchObject({ open: true, pendingRequestID: 41, form: fields });
  rerender({
    ...props,
    approvals: { data: [outboundApproval({ status: "completed" })] },
  });
  await waitFor(() => expect(result.current.compose.compose.open).toBe(false));
  expect(result.current.runner.state.message).toBe("Message accepted for SMTP delivery.");
});

it("fails closed when a pending action omits its request identity", async () => {
  post.mockImplementation(async (_path, payload) => {
    if (connectorActionRequest(payload).action_name === "send_message") {
      const response = actionResponse("send_message", { status: "approval_pending" });
      Reflect.deleteProperty(response, "request_id");
      return response;
    }
    return actionResponse(connectorActionRequest(payload).action_name);
  });
  const { result } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => result.current.openCompose());
  await act(async () =>
    result.current.compose.submitMessage({
      to: ["one@example.com"],
      cc: [],
      bcc: [],
      subject: "Status",
      text_body: "Ready",
      html_body: "",
    }),
  );

  expect(result.current.runner.state).toMatchObject({ state: "error", error: "Invalid connector action response from gateway." });
  expect(result.current.compose.compose.pendingRequestID).toBeUndefined();
});

it("ignores a Mail response that completes after the target scope changes", async () => {
  let resolveFolders: ((_value: ReturnType<typeof actionResponse>) => void) | undefined;
  post.mockImplementation((_path, payload) =>
    connectorActionRequest(payload).action_name === "list_folders"
      ? new Promise<ReturnType<typeof actionResponse>>((resolve) => {
          resolveFolders = resolve;
        })
      : Promise.resolve(actionResponse(connectorActionRequest(payload).action_name)),
  );
  const { result, rerender, props } = renderWorkspace();
  await waitFor(() => expect(resolveFolders).toBeTypeOf("function"));
  rerender({ ...props, target: { ...target, ref: "mail:2:2" }, session: { active: false, startedAt: "" } });

  await act(async () => {
    if (!resolveFolders) throw new Error("Folder request was not started.");
    resolveFolders(actionResponse("list_folders"));
  });
  expect(result.current.mailbox.folders).toEqual([]);
  expect(post.mock.calls.filter(([, payload]) => connectorActionRequest(payload).action_name === "search_messages")).toHaveLength(0);
});

it("allows a newer folder selection to supersede an in-flight message read", async () => {
  let resolveMessage: ((_value: ReturnType<typeof actionResponse>) => void) | undefined;
  post.mockImplementation(async (_path, payload) => {
    if (connectorActionRequest(payload).action_name === "get_message") {
      return new Promise<ReturnType<typeof actionResponse>>((resolve) => {
        resolveMessage = resolve;
      });
    }
    return actionResponse(connectorActionRequest(payload).action_name);
  });
  const { result } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => {
    void result.current.mailbox.selectMessage(message);
  });
  await waitFor(() => expect(resolveMessage).toBeTypeOf("function"));
  await act(async () => result.current.mailbox.selectFolder("Archive"));
  await act(async () => {
    if (!resolveMessage) throw new Error("Message request was not started.");
    resolveMessage(actionResponse("get_message"));
  });

  expect(result.current.mailbox.selectedFolder).toBe("Archive");
  expect(result.current.mailbox.selectedMessage).toBeNull();
});

it("derives inactive capabilities and outbound ownership from the current approval scope", () => {
  const scopedTarget = { ref: target.ref, public: { imap_enabled: false, smtp_auth_mode: "none" } };
  const { result } = renderWorkspace({
    target: scopedTarget,
    session: undefined,
    approvals: { data: [{ id: 72, target_ref: target.ref, action_name: "send_message", status: "running" }] },
  });
  expect(result.current.activeSession).toEqual({ active: false, startedAt: "" });
  expect(result.current.outboundPending).toBe(true);
  expect(result.current.destinationFolders).toEqual([]);
  expect(result.current.canArchive).toBe(false);
  expect(result.current.canDelete).toBe(false);
});

it("retains an unknown SMTP draft and requires explicit confirmation before retry", async () => {
  mockOutbound({
    status: "outcome_unknown",
    error: "SMTP result unknown",
    output: { submission_status: "submission_unknown", message_id: "test-message" },
  });
  const { result } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => result.current.openCompose());
  const fields = { to: ["one@example.test"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };
  await act(async () => result.current.compose.submitMessage(fields));
  expect(result.current.compose.compose).toMatchObject({ open: true, submissionUnknown: { messageID: "test-message" } });
  const count = post.mock.calls.length;
  await act(async () => result.current.compose.submitMessage({ ...fields, subject: "Changed" }));
  expect(post.mock.calls).toHaveLength(count);
  expect(result.current.compose.retryDialog).toMatchObject({ open: true, draftChanged: true, messageID: "test-message" });
  post.mockResolvedValue(connectorActionFixture({ target_ref: target.ref, action_name: "send_message" }));
  await act(async () => result.current.compose.confirmRetry());
  expect(result.current.compose.compose.open).toBe(false);
});

it("retains an approval-resolved unknown submission without silently resending", async () => {
  mockOutbound({ request_id: 41, status: "approval_pending" });
  const { result, rerender, props } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => result.current.openCompose());
  const fields = { to: ["one@example.test"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };
  await act(async () => result.current.compose.submitMessage(fields));
  rerender({
    ...props,
    approvals: {
      data: [
        outboundApproval({
          status: "outcome_unknown",
          output: { submission_status: "submission_unknown", message_id: "approved-message" },
        }),
      ],
    },
  });
  await waitFor(() => expect(result.current.compose.compose.submissionUnknown?.messageID).toBe("approved-message"));
  expect(result.current.compose.compose.form).toEqual(fields);
  const count = post.mock.calls.length;
  await act(async () => result.current.compose.submitMessage(fields));
  expect(post.mock.calls).toHaveLength(count);
  expect(result.current.compose.retryDialog).toMatchObject({ open: true, draftChanged: false });
});

function actionResponse(actionName: string, overrides: Partial<ReturnType<typeof connectorActionFixture>> = {}) {
  const outputs: Record<string, unknown> = {
    list_folders: { folders: [{ name: "INBOX" }, { name: "Archive" }], count: 2 },
    search_messages: { folder: "INBOX", messages: [message], count: 1, total: 1, unread: 1 },
    get_message: message,
    mark_read: { read: true },
  };
  return connectorActionFixture({
    target_ref: target.ref,
    connector_kind: "mail",
    action_name: actionName,
    output: outputs[actionName] || {},
    ...overrides,
  });
}

it.each(["completed", "failed", "outcome_unknown"] as const)(
  "keeps an outbound %s outcome separate from an in-flight Archive search",
  async (status) => {
    const { result, rerender, props } = renderWorkspace();
    await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
    act(() => result.current.openCompose());
    post.mockResolvedValueOnce(actionResponse("send_message", { request_id: 41, status: "approval_pending" }));
    const fields = { to: ["one@example.test"], cc: [], bcc: [], subject: "Review", text_body: "Ready", html_body: "" };
    await act(async () => result.current.compose.submitMessage(fields));
    act(() => result.current.compose.closeCompose());
    let finishSearch: ((_value: ReturnType<typeof actionResponse>) => void) | undefined;
    post.mockReturnValueOnce(
      new Promise((resolve) => {
        finishSearch = resolve;
      }),
    );
    let browsing: Promise<void> | undefined;
    act(() => {
      browsing = result.current.mailbox.selectFolder("Archive");
    });
    const signal = post.mock.calls.at(-1)?.[2]?.signal;
    const state = result.current.runner.state;
    const count = post.mock.calls.length;
    rerender({
      ...props,
      approvals: {
        data: [
          outboundApproval({
            status,
            error: status === "completed" ? "" : "SMTP outcome failed",
            output: status === "outcome_unknown" ? { submission_status: "submission_unknown", message_id: "unknown" } : {},
          }),
        ],
      },
    });
    await waitFor(() => expect(result.current.compose.outcome?.item?.status).toBe(status));
    expect(result.current.runner.state).toEqual(state);
    expect(result.current.runner.state.state).toBe("loading");
    expect(signal?.aborted).toBe(false);
    expect(post.mock.calls).toHaveLength(count);
    const outcome = result.current.compose.outcome;
    await act(async () => {
      finishSearch?.(actionResponse("search_messages"));
      await browsing;
    });
    expect(result.current.compose.outcome).toEqual(outcome);
    expect(result.current.mailbox.selectedFolder).toBe("Archive");
  },
);

it("does not replace a pending submission with a newer outbound attempt", async () => {
  const { result, rerender, props } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  act(() => result.current.openCompose());
  post.mockResolvedValueOnce(actionResponse("send_message", { request_id: 41, status: "approval_pending" }));
  const fields = { to: ["one@example.test"], cc: [], bcc: [], subject: "Review", text_body: "Ready", html_body: "" };
  await act(async () => result.current.compose.submitMessage(fields));
  const count = post.mock.calls.length;
  await act(async () => result.current.compose.submitMessage({ ...fields, subject: "Newer attempt" }));
  expect(post.mock.calls).toHaveLength(count);
  rerender({
    ...props,
    approvals: {
      data: [
        outboundApproval({
          status: "outcome_unknown",
          error: "Old SMTP result unknown",
          output: { submission_status: "submission_unknown", message_id: "old-message" },
        }),
      ],
    },
  });
  await waitFor(() => expect(result.current.runner.pendingActions).toEqual({}));
  expect(result.current.compose.outcome?.item?.status).toBe("outcome_unknown");
  expect(result.current.compose.compose.open).toBe(true);
  expect(result.current.compose.compose.submissionUnknown?.messageID).toBe("old-message");
  act(() => result.current.openCompose());
  expect(result.current.compose.compose.form).toEqual(fields);
});

it.each(["lost-response", "outcome_unknown"])("retains a canceled in-flight reply after %s", async (mode) => {
  const { result } = renderWorkspace();
  await waitFor(() => expect(result.current.mailbox.messages).toHaveLength(1));
  await act(async () => result.current.mailbox.selectMessage(message));
  act(() => result.current.compose.openReply());
  let finish: (() => void) | undefined;
  post.mockImplementationOnce(
    () =>
      new Promise((resolve, reject) => {
        finish = () =>
          mode === "lost-response"
            ? reject(new TypeError("Response lost after dispatch"))
            : resolve(actionResponse("reply_message", { status: "outcome_unknown", error: "SMTP result unknown" }));
      }),
  );
  const fields = { to: ["sender@example.com"], cc: [], bcc: [], subject: "Re: Status", text_body: "Ready", html_body: "" };
  let sending: Promise<void> | undefined;
  act(() => {
    sending = result.current.compose.submitMessage(fields);
  });
  act(() => result.current.compose.closeCompose());
  await act(async () => {
    finish?.();
    await sending;
  });
  expect(result.current.compose.compose).toMatchObject({ open: false, reply: true, messageRef: message.message_ref, form: fields });
  expect(result.current.compose.compose.submissionUnknown).toBeDefined();
  act(() => result.current.compose.openCompose());
  const count = post.mock.calls.length;
  await act(async () => result.current.compose.submitMessage(fields));
  expect(post.mock.calls).toHaveLength(count);
  expect(result.current.compose.retryDialog.open).toBe(true);
  await act(async () => result.current.compose.confirmRetry());
  expect(post.mock.calls).toHaveLength(count + 1);
  expect(connectorActionRequest(post.mock.calls.at(-1)?.[1])).toMatchObject({
    action_name: "reply_message",
    input: { ...fields, message_ref: message.message_ref },
  });
});
