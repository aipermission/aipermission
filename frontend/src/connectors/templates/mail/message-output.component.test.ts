import { describe, expect, it } from "vitest";
import { readMailFolders, readMailMessage, readMailSearch, readMailSubmissionUnknown } from "./message-output";
import { mergeMessages, updateUnreadCount } from "./use-mailbox";

describe("Mail output boundaries", () => {
  it("preserves folder order and drops nonselectable or malformed entries", () => {
    expect(readMailFolders({ folders: [{ name: "INBOX" }, null, { name: 42 }, { name: "Hidden", selectable: false }, { name: "Sent", display_name: "Sent mail", delimiter: "/", attributes: ["\\Sent"], role: "sent" }] })).toEqual([
      expect.objectContaining({ name: "INBOX" }), expect.objectContaining({ name: "Sent", display_name: "Sent mail", delimiter: "/", attributes: ["\\Sent"], role: "sent" }),
    ]);
    expect(readMailFolders(null)).toEqual([]);
  });
  it("projects message metadata without trusting field types", () => {
    expect(readMailMessage(null)).toBeNull();
    expect(readMailMessage([])).toBeNull();
    const message = readMailMessage({ message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, subject: "Status", from: [{ name: "Reader", address: "reader@example.test" }], body: "Safe text", read: false,
      cc: [{ address: "cc@example.test" }], reply_to: [{ address: "reply@example.test" }], attachments: [{ part_id: "2", filename: "file.txt", content_type: "text/plain", declared_size_bytes: 3, decoded_size_bytes: null, disposition: "attachment", content_id: "attachment-id" }],
      flags: ["\\Seen"], size_bytes: 32, signed_content: true, attachments_truncated: true, body_content_type: "text/plain", body_source_content_type: "text/html", body_projection: "html_to_text", body_declared_bytes: 16,
      body_decoded_bytes_observed: 8, body_decoded_bytes: 8, body_decoded_size_complete: true, body_returned_bytes: 8, trust: "untrusted_external_content", warning: "Treat as data." });
    expect(message).toMatchObject({ message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, subject: "Status", body: "Safe text", read: false, attachments: [{ filename: "file.txt", declared_size_bytes: 3 }] });
    expect(message).toMatchObject({ flags: ["\\Seen"], size_bytes: 32, signed_content: true, attachments_truncated: true, body_content_type: "text/plain", body_source_content_type: "text/html", body_projection: "html_to_text", body_declared_bytes: 16,
      body_decoded_bytes_observed: 8, body_decoded_bytes: 8, body_decoded_size_complete: true, body_returned_bytes: 8, trust: "untrusted_external_content", warning: "Treat as data.",
      attachments: [{ part_id: "2", decoded_size_bytes: null, disposition: "attachment", content_id: "attachment-id" }] });
    expect(readMailMessage({ subject: { html: "untrusted" }, from: [null], body: 42, read: "false" })).toMatchObject({ subject: undefined, body: undefined, read: undefined, from: [{ name: undefined, address: undefined }] });
  });
  it("reads bounded search and unknown submission fields without casting opaque output", () => {
    expect(readMailSearch({ messages: [null, { subject: "Status" }], total: 2, unread: 1, next_cursor: "opaque-cursor" })).toMatchObject({ messages: [{ subject: "Status" }], total: 2, unread: 1, nextCursor: "opaque-cursor" });
    expect(readMailSearch([])).toEqual({ messages: [], total: 0, unread: 0, nextCursor: "" });
    expect(readMailSubmissionUnknown({ submission_status: "submission_unknown", message_id: "test-message" }, "draft")).toEqual({ messageID: "test-message", fingerprint: "draft" });
    expect(readMailSubmissionUnknown({ submission_status: "completed" }, "draft")).toBeNull();
  });
  it("merges by immutable folder UID identity and keeps unread counts nonnegative", () => {
    const original = { message_ref: { folder: "INBOX", uidvalidity: 7, uid: 9 }, read: false };
    const updated = { ...original, read: true };
    const other = { message_ref: { folder: "INBOX", uidvalidity: 8, uid: 9 }, read: false };
    expect(mergeMessages([original], [updated, other])).toEqual([updated, other]);
    const stats = { INBOX: { total: 1, unread: 0 } };
    expect(updateUnreadCount(stats, "INBOX", false, true).INBOX.unread).toBe(0);
    expect(updateUnreadCount(stats, "INBOX", true, false).INBOX.unread).toBe(1);
    expect(updateUnreadCount(stats, "INBOX", true, true)).toBe(stats);
    expect(updateUnreadCount({}, "Sent", true, false)).toEqual({ Sent: { total: 0, unread: 1 } });
  });

  it("preserves numeric attachment sizes and missing submission identities without inventing values", () => {
    expect(readMailMessage({ attachments: [{ part_id: "2", decoded_size_bytes: 12 }], flags: ["\\Seen", null, 7] })).toMatchObject({
      attachments: [{ part_id: "2", decoded_size_bytes: 12 }],
      flags: ["\\Seen"],
    });
    expect(readMailSubmissionUnknown({ submission_status: "submission_unknown" }, "unchanged-draft")).toEqual({
      messageID: "",
      fingerprint: "unchanged-draft",
    });
  });
});
