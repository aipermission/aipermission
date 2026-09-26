import { test } from "vitest";
import assert from "node:assert/strict";
import { connectorActionCode, connectorActionError, connectorActionPending } from "../_shared/action-result.ts";
import {
  addressLabel,
  addressValues,
  formatMessageDate,
  mailActionResolution,
  mailActionSummary,
  mailFolderAllowed,
  mailFolderEqual,
  mailProtocolCapabilities,
  mailProtocolsEnabled,
  messageRefKey,
  recipientList,
  replySubject,
  replyText,
  submissionDraftFingerprint,
  unknownSubmissionRetryDecision,
  validateComposeFields,
} from "./helpers.ts";
import { normalizeEditorLink, plainTextToHTML, richTextToPlainText, splitPlainTextLines } from "./rich-text.ts";

test("mail helpers preserve stable message references and explicit read errors", () => {
  assert.equal(messageRefKey({ message_ref: { folder: "INBOX", uidvalidity: 42, uid: 7 } }), "INBOX:42:7");
  assert.equal(connectorActionError({ status: "completed" }), "");
  assert.equal(connectorActionError({ status: "failed", error: "IMAP failed" }), "IMAP failed");
  assert.equal(connectorActionError({ status: "error", display_text: "SMTP failed" }), "SMTP failed");
  assert.equal(connectorActionError({ status: "approval_pending" }), "");
  assert.equal(connectorActionError({ status: "running", display_text: "still running" }), "");
  assert.equal(connectorActionPending({ status: "approval_pending" }), true);
  assert.equal(connectorActionPending({ status: "completed" }), false);
  assert.equal(connectorActionCode({ output: { code: "stale_message_reference" } }), "stale_message_reference");
});

test("unknown SMTP retry guard is bound to the exact submitted draft", () => {
  const draft = { to: ["one@example.com"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };
  assert.equal(submissionDraftFingerprint(draft), submissionDraftFingerprint({ ...draft }));
  assert.notEqual(submissionDraftFingerprint(draft), submissionDraftFingerprint({ ...draft, text_body: "Changed" }));
  assert.deepEqual(unknownSubmissionRetryDecision(null, draft), { required: false, changed: false });
  const unknown = { fingerprint: submissionDraftFingerprint(draft) };
  assert.deepEqual(unknownSubmissionRetryDecision(unknown, draft), { required: true, changed: false });
  assert.deepEqual(unknownSubmissionRetryDecision(unknown, { ...draft, subject: "Changed" }), { required: true, changed: true });
});

test("compose validation mirrors bounded outbound limits before submission", () => {
  const valid = { to: ["one@example.com"], cc: [], bcc: [], subject: "Status", text_body: "Ready", html_body: "" };
  assert.equal(validateComposeFields(valid), "");
  assert.match(validateComposeFields({ ...valid, to: [] }), /To recipient/);
  assert.match(validateComposeFields({ ...valid, subject: "x".repeat(513) }), /512 bytes/);
  assert.match(validateComposeFields({ ...valid, subject: "x".repeat(509) }, { reply: true }), /512 bytes/);
  assert.match(validateComposeFields({ ...valid, text_body: "x".repeat(64 * 1024 + 1) }), /64 KiB/);
  assert.match(validateComposeFields({ ...valid, html_body: "x".repeat(128 * 1024 + 1) }), /128 KiB/);
});

test("pending Mail actions resolve exactly once from connector activity", () => {
  assert.equal(mailActionResolution([], 41), null);
  assert.equal(mailActionResolution([{ id: 41, status: "approval_pending" }], 41)?.state, "pending");
  assert.equal(mailActionResolution([{ id: 41, status: "running" }], 41)?.state, "pending");
  assert.equal(mailActionResolution([{ id: 41, status: "completed" }], 41)?.state, "completed");
  assert.equal(mailActionResolution([{ id: 41, status: "declined" }], 41)?.state, "failed");
});

test("mail helpers normalize recipients and reply labels", () => {
  assert.deepEqual(recipientList('"Doe, John" <john@example.com>, two@example.com\nthree@example.com'), [
    '"Doe, John" <john@example.com>',
    "two@example.com",
    "three@example.com",
  ]);
  assert.equal(addressLabel([{ name: "Operator", address: "operator@example.com" }]), "Operator <operator@example.com>");
  assert.equal(replySubject("Status"), "Re: Status");
  assert.equal(replySubject("Re: Status"), "Re: Status");
});

test("mail helpers keep action status compact and quote safe reply text", () => {
  assert.equal(
    mailActionSummary("search_messages", { output: { folder: "Sent", count: 2, total: 9 } }),
    "Sent loaded: 2 shown · 9 message(s) in mailbox.",
  );
  assert.equal(
    replyText({ from: [{ name: "Operator", address: "operator@example.com" }], body: "First line\r\nSecond line" }),
    "\n\nOn Unknown date, Operator <operator@example.com> wrote:\n> First line\n> Second line",
  );
  assert.equal(replyText({ body: "" }), "");
});

test("Mail folder policy mirrors backend INBOX matching", () => {
  assert.equal(mailFolderEqual("INBOX", "Inbox"), true);
  assert.equal(mailFolderEqual("Sent", "sent"), false);
  assert.equal(mailFolderAllowed("Inbox", ["INBOX", "Sent"]), true);
});

test("Mail workspace supports SMTP-only profiles without IMAP actions", () => {
  assert.deepEqual(mailProtocolCapabilities({ imap_enabled: false, smtp_auth_mode: "separate" }), {
    imapEnabled: false,
    smtpEnabled: true,
  });
  assert.deepEqual(mailProtocolCapabilities({ imap_enabled: true, smtp_auth_mode: "disabled" }), { imapEnabled: true, smtpEnabled: false });
  assert.equal(mailProtocolsEnabled({ imap_enabled: false, smtp_auth_mode: "disabled" }), false);
  assert.equal(mailProtocolsEnabled({ imap_enabled: false, smtp_auth_mode: "separate" }), true);
});

test("formatted editor preserves pasted lines and bounds link protocols", () => {
  assert.deepEqual(splitPlainTextLines("one\r\ntwo\nthree"), ["one", "two", "three"]);
  assert.equal(normalizeEditorLink("https://example.com/path"), "https://example.com/path");
  assert.equal(normalizeEditorLink("javascript:alert(1)"), "");
});

test("mail reply quotes remain within the outbound body budget", () => {
  const reply = replyText({ from: [{ address: "operator@example.com" }], body: "x".repeat(80 * 1024) });
  assert.ok(new TextEncoder().encode(reply).length <= 48 * 1024);
  assert.match(reply, /quoted message truncated/);
});

test("mail rich text produces a deterministic list-aware plain-text fallback", () => {
  const root = document.createElement("div");
  root.innerHTML = '<p>Hello <a href="https://example.com/docs">documentation</a></p><ol><li>First</li><li>Second</li></ol>';

  assert.equal(richTextToPlainText(root), "Hello documentation (https://example.com/docs)\n1. First\n2. Second");
  root.innerHTML = '<a href="https://example.com/empty"></a>';
  assert.equal(richTextToPlainText(root), "https://example.com/empty");
  assert.equal(plainTextToHTML('<hello>\n"team"'), "&lt;hello&gt;<br>&quot;team&quot;");
});

test("message and action projections tolerate missing and malformed external data", () => {
  assert.equal(messageRefKey(null), ":0:0");
  assert.equal(messageRefKey({ folder: "Sent", uid: 3 }), "Sent:0:3");
  assert.equal(addressLabel(null), "Unknown sender");
  assert.equal(addressLabel([{ name: "reader" }, { address: "one@example.test" }, {}]), "reader, one@example.test");
  assert.deepEqual(addressValues(null), []);
  assert.deepEqual(addressValues([{ address: "one@example.test" }, {}]), ["one@example.test"]);
  assert.equal(formatMessageDate("invalid"), "invalid");
  assert.notEqual(formatMessageDate(new Date("2026-01-01T00:00:00Z")), "Unknown date");
  assert.equal(mailActionSummary("list_folders", { output: null }), "Folders refreshed (0).");
  assert.equal(mailActionSummary("search_messages", { output: ["untrusted"] }), "Mailbox loaded: 0 shown · 0 message(s) in mailbox.");
  assert.equal(mailActionSummary("get_message", null), "Message loaded.");
  assert.equal(mailActionSummary("mark_read", null), "Message marked as read.");
  assert.equal(mailActionSummary("mark_unread", null), "Message marked as unread.");
  assert.equal(mailActionSummary("move_message", null), "Message moved.");
  assert.equal(mailActionSummary("archive_message", null), "Message archived.");
  assert.equal(mailActionSummary("delete_message", null), "Message moved to Trash.");
  assert.equal(mailActionSummary("send_message", null), "Message accepted for SMTP delivery.");
  assert.equal(mailActionSummary("reply_message", null), "Reply accepted for SMTP delivery.");
  assert.equal(mailActionSummary("custom_action", null), "custom action completed.");
  assert.equal(mailActionResolution(null, 1), null);
  assert.equal(mailActionResolution([], 0), null);
  assert.equal(mailFolderAllowed("INBOX", null), false);
});

test("compose byte limits and quoting remain deterministic for unicode and escaped display names", () => {
  assert.deepEqual(recipientList('"Doe, \\"John\\"" <one@example.test>; two@example.test'), ['"Doe, \\"John\\"" <one@example.test>', "two@example.test"]);
  const valid = { to: "one@example.test", subject: "Status", text_body: "Ready" };
  assert.match(validateComposeFields({ ...valid, cc: Array.from({ length: 20 }, () => "two@example.test") }), /20 recipients/);
  assert.match(validateComposeFields({ ...valid, to: ["x".repeat(321)] }), /320 bytes/);
  assert.match(validateComposeFields({ ...valid, subject: "line\nbreak" }), /one line/);
  assert.match(validateComposeFields({ ...valid, subject: "" }), /required/);
  assert.match(validateComposeFields({ ...valid, text_body: " " }), /required/);
  const reply = replyText({ body: "😀".repeat(18000) });
  assert.ok(new TextEncoder().encode(reply).length <= 48 * 1024);
  assert.equal(reply.includes("�"), false);
});
