export function messageRefKey(messageOrRef) {
  const ref = messageOrRef?.message_ref || messageOrRef || {};
  return `${ref.folder || ""}:${ref.uidvalidity || 0}:${ref.uid || 0}`;
}

export function addressLabel(addresses) {
  if (!Array.isArray(addresses) || addresses.length === 0) return "Unknown sender";
  return addresses
    .map((item) => {
      const name = String(item?.name || "").trim();
      const address = String(item?.address || "").trim();
      if (name && address) return `${name} <${address}>`;
      return name || address;
    })
    .filter(Boolean)
    .join(", ");
}

export function addressValues(addresses) {
  if (!Array.isArray(addresses)) return [];
  return addresses.map((item) => item?.address || "").filter(Boolean);
}

export function formatMessageDate(value) {
  if (!value) return "Unknown date";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

export function recipientList(value) {
  const source = String(value || "");
  const result = [];
  let current = "";
  let quoted = false;
  let escaped = false;
  let angleDepth = 0;
  for (const character of source) {
    if (escaped) {
      current += character;
      escaped = false;
      continue;
    }
    if (quoted && character === "\\") {
      current += character;
      escaped = true;
      continue;
    }
    if (character === '"') quoted = !quoted;
    if (!quoted && character === "<") angleDepth += 1;
    if (!quoted && character === ">" && angleDepth > 0) angleDepth -= 1;
    if (!quoted && angleDepth === 0 && (character === "," || character === ";" || character === "\n")) {
      if (current.trim()) result.push(current.trim());
      current = "";
      continue;
    }
    current += character;
  }
  if (current.trim()) result.push(current.trim());
  return result;
}

export function mailProtocolsEnabled(form) {
  return form?.imap_enabled !== false || (form?.smtp_auth_mode || "disabled") !== "disabled";
}

export function submissionDraftFingerprint(fields) {
  const normalized = {
    to: normalizeFingerprintRecipients(fields?.to),
    cc: normalizeFingerprintRecipients(fields?.cc),
    bcc: normalizeFingerprintRecipients(fields?.bcc),
    subject: String(fields?.subject || ""),
    text_body: String(fields?.text_body || ""),
    html_body: String(fields?.html_body || ""),
  };
  return JSON.stringify(normalized);
}

export function unknownSubmissionRetryDecision(submissionUnknown, fields) {
  if (!submissionUnknown) return { required: false, changed: false };
  return {
    required: true,
    changed: submissionUnknown.fingerprint !== submissionDraftFingerprint(fields),
  };
}

export function validateComposeFields(fields, { reply = false } = {}) {
  const recipients = [fields?.to, fields?.cc, fields?.bcc].flatMap((value) => (Array.isArray(value) ? value : recipientList(value)));
  if ((Array.isArray(fields?.to) ? fields.to : recipientList(fields?.to)).length === 0) return "Add at least one To recipient.";
  if (recipients.length > 20) return "A message can contain at most 20 recipients.";
  if (recipients.some((value) => utf8Length(String(value).trim()) > 320)) return "Each recipient address must not exceed 320 bytes.";
  const subject = String(fields?.subject || "").trim();
  if (!subject) return "Subject is required.";
  if (/[\r\n]/.test(subject)) return "Subject must stay on one line.";
  const preparedSubject = reply && !/^re:/i.test(subject) ? `Re: ${subject}` : subject;
  if (utf8Length(preparedSubject) > 512) return "Prepared subject must not exceed 512 bytes.";
  const textBody = String(fields?.text_body || "");
  if (!textBody.trim()) return "Message body is required.";
  if (utf8Length(textBody) > 64 * 1024) return "Plain-text message must not exceed 64 KiB.";
  if (utf8Length(String(fields?.html_body || "")) > 128 * 1024) return "Formatted message must not exceed 128 KiB.";
  return "";
}

export function mailActionResolution(items, requestID) {
  if (!requestID) return null;
  const item = (Array.isArray(items) ? items : []).find((candidate) => Number(candidate?.id) === Number(requestID));
  if (!item) return null;
  if (item.status === "approval_pending" || item.status === "running") return { state: "pending", item };
  if (item.status === "completed") return { state: "completed", item };
  return { state: "failed", item };
}

function normalizeFingerprintRecipients(value) {
  return (Array.isArray(value) ? value : recipientList(value)).map((item) => String(item).trim());
}

function utf8Length(value) {
  return new TextEncoder().encode(value).length;
}

export function replySubject(subject) {
  const value = String(subject || "").trim();
  return /^re:/i.test(value) ? value : `Re: ${value}`;
}

export function replyText(message) {
  const body = String(message?.body || "")
    .replace(/\r\n?/g, "\n")
    .trimEnd();
  if (!body) return "";
  const sender = addressLabel(message?.from);
  const date = formatMessageDate(message?.received_at || message?.header_date);
  const quote = body
    .split("\n")
    .map((line) => `> ${line}`)
    .join("\n");
  return boundedUTF8(`\n\nOn ${date}, ${sender} wrote:\n${quote}`, 48 * 1024, "\n> [quoted message truncated]");
}

function boundedUTF8(value, maxBytes, suffix) {
  const encoder = new TextEncoder();
  const encoded = encoder.encode(value);
  if (encoded.length <= maxBytes) return value;
  const suffixBytes = encoder.encode(suffix);
  const available = Math.max(0, maxBytes - suffixBytes.length);
  let boundary = available;
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let prefix = "";
  while (boundary > 0) {
    try {
      prefix = decoder.decode(encoded.slice(0, boundary));
      break;
    } catch {
      boundary -= 1;
    }
  }
  return `${prefix}${suffix}`;
}

export function mailActionSummary(actionName, item) {
  const output = item?.output || {};
  switch (actionName) {
    case "list_folders":
      return `Folders refreshed (${Number(output.count || 0)}).`;
    case "search_messages":
      return `${output.folder || "Mailbox"} loaded: ${Number(output.count || 0)} shown · ${Number(output.total || 0)} message(s) in mailbox.`;
    case "get_message":
      return "Message loaded.";
    case "mark_read":
      return "Message marked as read.";
    case "mark_unread":
      return "Message marked as unread.";
    case "move_message":
      return "Message moved.";
    case "archive_message":
      return "Message archived.";
    case "delete_message":
      return "Message moved to Trash.";
    case "send_message":
      return "Message accepted for SMTP delivery.";
    case "reply_message":
      return "Reply accepted for SMTP delivery.";
    default:
      return `${String(actionName || "Mail action").replaceAll("_", " ")} completed.`;
  }
}

export function mailFolderEqual(left, right) {
  const first = String(left || "");
  const second = String(right || "");
  return first === second || (first.toUpperCase() === "INBOX" && second.toUpperCase() === "INBOX");
}

export function mailFolderAllowed(folder, allowed) {
  return Array.isArray(allowed) && allowed.some((candidate) => mailFolderEqual(candidate, folder));
}

export function mailProtocolCapabilities(publicProfile) {
  return {
    imapEnabled: publicProfile?.imap_enabled !== false,
    smtpEnabled: publicProfile?.smtp_auth_mode !== "disabled",
  };
}
