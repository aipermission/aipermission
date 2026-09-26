import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MessageDetail } from "./message-detail";
import { FolderPane, MessagePane } from "./mailbox-pane";
import { DeleteMessageDialog, MoveMessageDialog, RetryUnknownSubmissionDialog } from "./message-dialogs";
import { MailActionResultDialog } from "./action-result-dialog";

const styles = { borderClass: "", mutedClass: "", rowHoverClass: "", activeRowClass: "active" };
describe("Mail message presentation contracts", () => {
  it("renders external mail as text without executing HTML and preserves read controls", () => {
    const toggleRead = vi.fn();
    const props = {
      ...styles,
      subtlePanelClass: "",
      busy: false,
      canReply: false,
      canMove: false,
      canArchive: false,
      canDelete: false,
      onToggleRead: toggleRead,
      onReply: vi.fn(),
      onMove: vi.fn(),
      onArchive: vi.fn(),
      onDelete: vi.fn(),
    };
    const { container, rerender } = render(<MessageDetail {...props} message={null} />);
    expect(screen.getByText(/Select a message/)).toBeInTheDocument();
    rerender(
      <MessageDetail
        {...props}
        message={{
          subject: "Status",
          body: '<img src="https://example.test/private" onerror="alert(1)">',
          read: false,
          from: [{ address: "reader@example.test" }],
          attachments: [{ filename: "report.txt", content_type: "text/plain", declared_size_bytes: 2 }],
        }}
      />,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText(/report.txt/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    expect(toggleRead).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: /Reply unavailable/ })).toBeDisabled();
    rerender(<MessageDetail {...props} message={{ read: true, body_available: false }} />);
    expect(screen.getByText("No safe text body is available.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mark unread" })).toBeInTheDocument();
  });

  it("preserves server folder order and selects exact message references", () => {
    const onFolder = vi.fn();
    const { unmount } = render(
      <FolderPane
        {...styles}
        folders={[{ name: "INBOX" }, { name: "Sent" }]}
        selectedFolder="INBOX"
        folderStats={{ INBOX: { unread: 2 } }}
        onSelect={onFolder}
      />,
    );
    expect(screen.getAllByRole("button").map((button) => button.textContent)).toEqual(["INBOX2", "Sent"]);
    fireEvent.click(screen.getByRole("button", { name: "Sent" }));
    expect(onFolder).toHaveBeenCalledWith("Sent");
    unmount();
    const message = { subject: "Status", read: false, message_ref: { folder: "INBOX", uid: 3, uidvalidity: 5 } };
    const onSelect = vi.fn();
    render(
      <MessagePane
        {...styles}
        inputClass=""
        messages={[message]}
        selectedRef="INBOX:5:3"
        query=""
        unreadOnly={false}
        hasMore={false}
        busy={false}
        onSelect={onSelect}
        onQuery={vi.fn()}
        onUnreadOnly={vi.fn()}
        onSearch={vi.fn()}
        onLoadMore={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Status/ }));
    expect(onSelect).toHaveBeenCalledWith(message);
    expect(screen.getByRole("button", { name: "No more messages" })).toBeDisabled();
  });

  it("excludes only the source folder and requires an explicit move destination", () => {
    const onDestination = vi.fn();
    const props = {
      busy: false,
      onClose: vi.fn(),
      onConfirm: vi.fn(),
      onDestination,
      folders: [{ name: "Inbox" }, { name: "Sent" }],
      dialog: { open: true, sourceFolder: "INBOX", destination: "" },
    };
    const { rerender } = render(<MoveMessageDialog {...props} />);
    expect(screen.queryByRole("option", { name: "Inbox" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Move message" })).toBeDisabled();
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "Sent" } });
    expect(onDestination).toHaveBeenCalledWith("Sent");
    rerender(<MoveMessageDialog {...props} dialog={{ ...props.dialog, destination: "Sent" }} />);
    fireEvent.click(screen.getByRole("button", { name: "Move message" }));
    expect(props.onConfirm).toHaveBeenCalledTimes(1);
  });

  it("shows duplicate-delivery and non-expunge warnings before destructive actions", () => {
    const props = { busy: false, onClose: vi.fn(), onConfirm: vi.fn() };
    const { unmount } = render(<DeleteMessageDialog {...props} open trashFolder="Trash" />);
    expect(screen.getByText(/never performs permanent IMAP/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Move to Trash" }));
    expect(props.onConfirm).toHaveBeenCalledTimes(1);
    unmount();
    render(<RetryUnknownSubmissionDialog {...props} value={{ open: true, draftChanged: true, messageID: "test-message" }} />);
    expect(screen.getByText(/draft changed/)).toBeInTheDocument();
    expect(screen.getByText(/test-message/)).toBeInTheDocument();
  });

  it("renders bounded raw action data in a dialog rather than as HTML", () => {
    render(
      <MailActionResultDialog
        onClose={vi.fn()}
        value={{
          open: true,
          actionName: "get_message",
          summary: "Message loaded.",
          item: { status: "completed", output: { subject: "Status" } },
        }}
      />,
    );
    expect(screen.getByRole("dialog")).toHaveTextContent('"subject": "Status"');
    expect(screen.getByRole("dialog")).toHaveTextContent('"action_name": "get_message"');
  });
});
