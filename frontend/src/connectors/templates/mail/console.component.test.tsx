import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { MailConnectorConsoleTemplate } from "./console";
import { connectorActionFixture, connectorActionRequest, connectorApprovalFixture } from "../../../test/connector-action-fixtures";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";

const api = vi.hoisted(() => ({ post: vi.fn() }));
vi.mock("../../../lib/api", () => ({ apiPost: api.post, apiPut: vi.fn(), apiDelete: vi.fn() }));
const props: ComponentProps<typeof MailConnectorConsoleTemplate> = {
  target: gatewayTargetFixture({
    connector_kind: "mail",
    ref: "mail:1:1",
    profile_label: "mailbox",
    public: { imap_enabled: true, smtp_auth_mode: "disabled" },
  }),
  session: null,
  approvals: { state: "ready", data: [], error: null },
  theme: "dark",
  onNewStructuredSession: vi.fn(),
  onRefreshActivity: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
});

it("opens no connector action until the local structured session starts", () => {
  render(<MailConnectorConsoleTemplate {...props} />);
  expect(screen.getByText("No active Mail session")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Start Mail session" }));
  expect(props.onNewStructuredSession).toHaveBeenCalledTimes(1);
  expect(api.post).not.toHaveBeenCalled();
});

it("renders the server folder and message projections in an active IMAP session", async () => {
  api.post.mockImplementation(async (_path: string, payload: { target_ref: string; action_name: string }) =>
    connectorActionFixture({
      target_ref: payload.target_ref,
      action_name: payload.action_name,
      output:
        payload.action_name === "list_folders"
          ? { folders: [{ name: "INBOX" }], count: 1 }
          : { messages: [{ subject: "Status", message_ref: { folder: "INBOX", uidvalidity: 1, uid: 1 } }], total: 1, unread: 1 },
    }),
  );
  render(<MailConnectorConsoleTemplate {...props} session={{ active: true, startedAt: "now" }} />);
  await waitFor(() => expect(screen.getByRole("button", { name: /Status/ })).toBeInTheDocument());
  expect(screen.getByRole("button", { name: "Compose" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh mailbox" })).toBeInTheDocument();
});

it("shows the SMTP-only workspace without issuing mailbox reads", () => {
  render(
    <MailConnectorConsoleTemplate
      {...props}
      target={{ ...props.target, public: { imap_enabled: false, smtp_auth_mode: "separate" } }}
      theme="light"
      session={{ active: true, startedAt: "now" }}
    />,
  );
  expect(screen.getByText("SMTP-only Mail profile")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Refresh mailbox" })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Compose message" }));
  expect(screen.getByRole("dialog", { name: "Compose message" })).toBeInTheDocument();
  expect(api.post).not.toHaveBeenCalled();
});

it.each(
  ["send_message", "reply_message"].flatMap((actionName) =>
    (["completed", "failed", "outcome_unknown"] as const).map((status) => ({ actionName, status })),
  ),
)("shows pending $actionName $status after closing compose and browsing Archive", async ({ actionName, status }) => {
  const message = { subject: "Status", message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, read: false };
  api.post.mockImplementation(async (_path, payload) => {
    const request = connectorActionRequest(payload);
    const output =
      request.action_name === "list_folders"
        ? { folders: [{ name: "INBOX" }, { name: "Archive" }] }
        : request.action_name === "get_message"
          ? message
          : { messages: [{ ...message, subject: request.input.folder === "Archive" ? "Archived" : "Status" }], total: 1, unread: 1 };
    return connectorActionFixture({
      target_ref: request.target_ref,
      action_name: request.action_name,
      output,
      ...(request.action_name === actionName ? { status: "approval_pending", request_id: 41 } : {}),
    });
  });
  const activeProps = {
    ...props,
    target: { ...props.target, public: { imap_enabled: true, smtp_auth_mode: "separate" } },
    session: { active: true, startedAt: "now" },
  };
  const { rerender } = render(<MailConnectorConsoleTemplate {...activeProps} />);
  await waitFor(() => expect(screen.getByRole("button", { name: /Status/ })).toBeInTheDocument());
  if (actionName === "reply_message") {
    fireEvent.click(screen.getByRole("button", { name: /Status/ }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Reply" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Reply" }));
  } else fireEvent.click(screen.getByRole("button", { name: "Compose" }));
  const title = actionName === "reply_message" ? "Reply" : "Compose message";
  const form = within(screen.getByRole("dialog", { name: title }));
  fireEvent.change(form.getByLabelText("To"), { target: { value: "one@example.test" } });
  fireEvent.change(form.getByLabelText("Subject"), { target: { value: "Review" } });
  fireEvent.change(form.getByLabelText("Message"), { target: { value: "Ready" } });
  fireEvent.click(form.getByRole("button", { name: actionName === "reply_message" ? "Send reply" : "Send message" }));
  await waitFor(() => expect(form.getByRole("button", { name: "Submitting..." })).toBeDisabled());
  await waitFor(() => expect(screen.getByText("Mail action is awaiting approval.")).toBeInTheDocument());
  fireEvent.click(form.getByRole("button", { name: "Cancel" }));
  fireEvent.click(screen.getByRole("button", { name: "Archive" }));
  await waitFor(() => expect(screen.getByRole("button", { name: /Archived/ })).toBeInTheDocument());
  const count = api.post.mock.calls.length;
  const error = status === "failed" ? "SMTP rejected" : status === "outcome_unknown" ? "SMTP result unknown" : "";
  rerender(
    <MailConnectorConsoleTemplate
      {...activeProps}
      approvals={{
        state: "ready",
        error: null,
        data: [
          connectorApprovalFixture({
            target_ref: "mail:1:1",
            action_name: actionName,
            id: 41,
            status,
            error,
            output: status === "outcome_unknown" ? { submission_status: "submission_unknown", message_id: "approved-message" } : {},
          }),
        ],
      }}
    />,
  );
  if (status === "completed") {
    const summary = actionName === "reply_message" ? "Reply accepted for SMTP delivery." : "Message accepted for SMTP delivery.";
    await waitFor(() => expect(screen.getByRole("button", { name: summary })).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: summary }));
    expect(screen.getByRole("dialog", { name: "Mail action result" })).toHaveTextContent(actionName);
  } else {
    const reopened = within(await screen.findByRole("dialog", { name: title }));
    expect(reopened.getByText(error)).toBeInTheDocument();
    expect(reopened.getByLabelText("Subject")).toHaveValue("Review");
    if (status === "outcome_unknown") {
      fireEvent.click(reopened.getByRole("button", { name: actionName === "reply_message" ? "Send reply" : "Send message" }));
      const retry = within(await screen.findByRole("dialog", { name: "Retry an unknown SMTP submission?" }));
      expect(retry.getByText("Message-ID: approved-message")).toBeInTheDocument();
      expect(api.post.mock.calls).toHaveLength(count);
      fireEvent.click(retry.getByRole("button", { name: "Cancel" }));
      expect(screen.queryByRole("dialog", { name: "Retry an unknown SMTP submission?" })).not.toBeInTheDocument();
      expect(api.post.mock.calls).toHaveLength(count);
      fireEvent.click(reopened.getByRole("button", { name: actionName === "reply_message" ? "Send reply" : "Send message" }));
      api.post.mockResolvedValueOnce(connectorActionFixture({ target_ref: "mail:1:1", action_name: actionName }));
      fireEvent.click(
        within(screen.getByRole("dialog", { name: "Retry an unknown SMTP submission?" })).getByRole("button", { name: "Retry anyway" }),
      );
      const summary = actionName === "reply_message" ? "Reply accepted for SMTP delivery." : "Message accepted for SMTP delivery.";
      await waitFor(() => expect(screen.getByRole("button", { name: summary })).toBeInTheDocument());
      expect(screen.queryByRole("dialog", { name: title })).not.toBeInTheDocument();
      expect(screen.queryByRole("dialog", { name: "Retry an unknown SMTP submission?" })).not.toBeInTheDocument();
      expect(api.post.mock.calls).toHaveLength(count + 1);
      expect(connectorActionRequest(api.post.mock.calls.at(-1)?.[1])).toEqual({
        target_ref: "mail:1:1",
        action_name: actionName,
        input: {
          to: ["one@example.test"],
          cc: [],
          bcc: [],
          subject: "Review",
          text_body: "Ready",
          html_body: "",
          ...(actionName === "reply_message" ? { message_ref: message.message_ref } : {}),
        },
      });
      return;
    }
  }
  expect(api.post.mock.calls).toHaveLength(count);
});

function renderMailbox() {
  const message = { subject: "Status", message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, read: false };
  const older = { ...message, subject: "Earlier deployment", message_ref: { ...message.message_ref, uid: 10 } };
  let moved = false;
  api.post.mockImplementation(async (_path, payload) => {
    const request = connectorActionRequest(payload);
    let output: unknown = {};
    if (request.action_name === "list_folders") output = { folders: [{ name: "INBOX" }, { name: "Archive" }, { name: "Trash" }] };
    else if (request.action_name === "get_message") output = message;
    else if (request.action_name === "search_messages") {
      const messages = moved
        ? []
        : request.input.cursor
          ? [older]
          : [{ ...message, subject: request.input.subject ? "Deployment" : "Status" }];
      output = {
        messages,
        count: messages.length,
        total: moved ? 0 : 2,
        unread: moved ? 0 : 2,
        next_cursor: moved || request.input.cursor ? "" : "page-2",
      };
    } else if (["move_message", "archive_message", "delete_message"].includes(request.action_name)) moved = true;
    return connectorActionFixture({ target_ref: request.target_ref, action_name: request.action_name, output });
  });
  render(
    <MailConnectorConsoleTemplate
      {...props}
      target={{
        ...props.target,
        public: {
          imap_enabled: true,
          smtp_auth_mode: "separate",
          archive_folder: "Archive",
          trash_folder: "Trash",
          allowed_mutation_destination_folders: ["Archive", "Trash"],
        },
      }}
      session={{ active: true, startedAt: "now" }}
    />,
  );
  return { message, requests: () => api.post.mock.calls.map(([, payload]) => connectorActionRequest(payload)) };
}

it("searches trimmed subjects, appends the cursor page, changes unread filter and refreshes the same view", async () => {
  const { requests } = renderMailbox();
  await waitFor(() => expect(screen.getByRole("button", { name: /Status/ })).toBeInTheDocument());
  const query = screen.getByPlaceholderText("Search subject");
  fireEvent.change(query, { target: { value: "  Deployment  " } });
  const form = query.closest("form");
  if (!form) throw new Error("Message search form missing.");
  expect(fireEvent.submit(form)).toBe(false);
  await waitFor(() => expect(screen.getByRole("button", { name: /^Deployment/ })).toBeInTheDocument());
  expect(requests().at(-1)).toEqual({
    target_ref: "mail:1:1",
    action_name: "search_messages",
    input: { folder: "INBOX", unread_only: true, subject: "Deployment", limit: 50, cursor: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Load older messages" }));
  await waitFor(() => expect(screen.getByRole("button", { name: /Earlier deployment/ })).toBeInTheDocument());
  expect(screen.getByRole("button", { name: /^Deployment/ })).toBeInTheDocument();
  expect(requests().at(-1)?.input).toEqual({ folder: "INBOX", unread_only: true, subject: "Deployment", limit: 50, cursor: "page-2" });
  expect(screen.getByRole("button", { name: "No more messages" })).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: "Unread only" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: /Earlier deployment/ })).not.toBeInTheDocument());
  expect(requests().at(-1)?.input).toEqual({ folder: "INBOX", unread_only: false, subject: "Deployment", limit: 50, cursor: "" });
  const count = requests().length;
  fireEvent.click(screen.getByRole("button", { name: "Refresh mailbox" }));
  await waitFor(() => expect(requests()).toHaveLength(count + 2));
  await waitFor(() => expect(screen.getByRole("button", { name: "Refresh mailbox" })).toBeEnabled());
  expect(
    requests()
      .slice(-2)
      .map((request) => request.action_name),
  ).toEqual(["list_folders", "search_messages"]);
  expect(requests().at(-1)?.input).toEqual({ folder: "INBOX", unread_only: false, subject: "Deployment", limit: 50, cursor: "" });
  fireEvent.click(screen.getByRole("button", { name: /Mailbox loaded:/ }));
  const result = within(screen.getByRole("dialog", { name: "Mail action result" }));
  expect(result.getByText(/search_messages/)).toBeInTheDocument();
  fireEvent.click(result.getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog", { name: "Mail action result" })).not.toBeInTheDocument();
});

it.each(["move_message", "archive_message", "delete_message"])(
  "submits %s for the selected reference and refreshes only after deliberate confirmation",
  async (actionName) => {
    const { message, requests } = renderMailbox();
    await waitFor(() => expect(screen.getByRole("button", { name: /Status/ })).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Status/ }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Move" })).toBeEnabled());
    const count = requests().length;
    if (actionName === "archive_message") fireEvent.click(screen.getByTitle("Archive"));
    else {
      const trigger = actionName === "move_message" ? "Move" : "Move to Trash";
      const title = actionName === "move_message" ? "Move message" : "Move message to Trash";
      fireEvent.click(screen.getByRole("button", { name: trigger }));
      let dialog = within(screen.getByRole("dialog", { name: title }));
      expect(requests()).toHaveLength(count);
      fireEvent.click(dialog.getByRole("button", { name: "Cancel" }));
      expect(screen.queryByRole("dialog", { name: title })).not.toBeInTheDocument();
      expect(requests()).toHaveLength(count);
      fireEvent.click(screen.getByRole("button", { name: trigger }));
      dialog = within(screen.getByRole("dialog", { name: title }));
      if (actionName === "move_message") {
        expect(dialog.getByRole("button", { name: "Move message" })).toBeDisabled();
        expect(dialog.queryByRole("option", { name: "INBOX" })).not.toBeInTheDocument();
        fireEvent.change(dialog.getByRole("combobox", { name: "Destination folder" }), { target: { value: "Archive" } });
      }
      fireEvent.click(dialog.getByRole("button", { name: actionName === "move_message" ? "Move message" : "Move to Trash" }));
    }
    await waitFor(() => expect(screen.getByText("No messages match this view.")).toBeInTheDocument());
    expect(requests()).toHaveLength(count + 2);
    expect(requests().at(-2)).toEqual({
      target_ref: "mail:1:1",
      action_name: actionName,
      input: { message_ref: message.message_ref, ...(actionName === "move_message" ? { destination_folder: "Archive" } : {}) },
    });
    expect(requests().at(-1)?.input).toEqual({ folder: "INBOX", unread_only: true, subject: "", limit: 50, cursor: "" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "INBOX" })).toBeInTheDocument();
  },
);
