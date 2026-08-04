import test from "node:test";
import assert from "node:assert/strict";
import { connectorActionCode, connectorActionError, connectorActionPending } from "../_shared/action-result.js";
import {
  addressLabel,
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
} from "./helpers.js";
import { normalizeEditorLink, plainTextToHTML, richTextToPlainText, splitPlainTextLines } from "./rich-text.js";

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
  assert.equal(mailActionResolution([{ id: 41, status: "approval_pending" }], 41).state, "pending");
  assert.equal(mailActionResolution([{ id: 41, status: "running" }], 41).state, "pending");
  assert.equal(mailActionResolution([{ id: 41, status: "completed" }], 41).state, "completed");
  assert.equal(mailActionResolution([{ id: 41, status: "declined" }], 41).state, "failed");
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
  const text = (value) => ({ nodeType: 3, nodeValue: value });
  const element = (tagName, childNodes = [], attributes = {}) => {
    const node = { nodeType: 1, tagName: tagName.toUpperCase(), childNodes, getAttribute: (name) => attributes[name] || "" };
    for (const child of childNodes) child.parentNode = node;
    node.children = childNodes.filter((child) => child.nodeType === 1);
    return node;
  };
  const first = element("li", [text("First")]);
  const second = element("li", [text("Second")]);
  const list = element("ol", [first, second]);
  first.parentElement = list;
  second.parentElement = list;
  const root = element("div", [
    element("p", [text("Hello "), element("a", [text("documentation")], { href: "https://example.com/docs" })]),
    list,
  ]);

  assert.equal(richTextToPlainText(root), "Hello documentation (https://example.com/docs)\n1. First\n2. Second");
  assert.equal(richTextToPlainText(element("div", [element("a", [], { href: "https://example.com/empty" })])), "https://example.com/empty");
  assert.equal(plainTextToHTML('<hello>\n"team"'), "&lt;hello&gt;<br>&quot;team&quot;");
});
