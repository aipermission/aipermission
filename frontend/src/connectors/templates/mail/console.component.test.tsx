import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { MailConnectorConsoleTemplate } from "./console";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";

const api = vi.hoisted(() => ({ post: vi.fn() }));
vi.mock("../../../lib/api", () => ({ apiPost: api.post, apiPut: vi.fn(), apiDelete: vi.fn() }));
const props: ComponentProps<typeof MailConnectorConsoleTemplate> = {
  target: { id: 1, name: "mail", connector_kind: "mail", ref: "mail:1:1", profile_label: "mailbox", public: { imap_enabled: true, smtp_auth_mode: "disabled" } },
  approvals: { data: [] }, theme: "dark", onNewStructuredSession: vi.fn(), onRefreshActivity: vi.fn(),
};

beforeEach(() => { vi.clearAllMocks(); });

it("opens no connector action until the local structured session starts", () => {
  render(<MailConnectorConsoleTemplate {...props} />);
  expect(screen.getByText("No active Mail session")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Start Mail session" }));
  expect(props.onNewStructuredSession).toHaveBeenCalledTimes(1);
  expect(api.post).not.toHaveBeenCalled();
});

it("renders the server folder and message projections in an active IMAP session", async () => {
  api.post.mockImplementation(async (_path: string, payload: { target_ref: string; action_name: string }) => connectorActionFixture({
    target_ref: payload.target_ref, action_name: payload.action_name,
    output: payload.action_name === "list_folders" ? { folders: [{ name: "INBOX" }], count: 1 } : { messages: [{ subject: "Status", message_ref: { folder: "INBOX", uidvalidity: 1, uid: 1 } }], total: 1, unread: 1 },
  }));
  render(<MailConnectorConsoleTemplate {...props} session={{ active: true, startedAt: "now" }} />);
  await waitFor(() => expect(screen.getByRole("button", { name: /Status/ })).toBeInTheDocument());
  expect(screen.getByRole("button", { name: "Compose" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh mailbox" })).toBeInTheDocument();
});

it("shows the SMTP-only workspace without issuing mailbox reads", () => {
  render(<MailConnectorConsoleTemplate {...props} target={{ ...props.target, public: { imap_enabled: false, smtp_auth_mode: "separate" } }} theme="light" session={{ active: true, startedAt: "now" }} />);
  expect(screen.getByText("SMTP-only Mail profile")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Refresh mailbox" })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Compose message" }));
  expect(screen.getByRole("dialog", { name: "Compose message" })).toBeInTheDocument();
  expect(api.post).not.toHaveBeenCalled();
});
