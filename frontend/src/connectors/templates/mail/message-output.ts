import type { MailAddress, MailAttachment, MailFolder, MailMessage, MailMessageRef } from "./message-types";

export function readMailRecord(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

export function readMailMessage(value: unknown): MailMessage | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = readMailRecord(value);
  return {
    ...item,
    ...messageReference(item),
    message_ref: item.message_ref ? messageReference(readMailRecord(item.message_ref)) : undefined,
    subject: string(item.subject), from: addresses(item.from), to: addresses(item.to), cc: addresses(item.cc), reply_to: addresses(item.reply_to),
    received_at: string(item.received_at), header_date: string(item.header_date), message_id: string(item.message_id),
    read: boolean(item.read), body: string(item.body), body_available: boolean(item.body_available), body_truncated: boolean(item.body_truncated),
    encrypted_content: boolean(item.encrypted_content), attachment_count: number(item.attachment_count), attachments: attachments(item.attachments),
    flags: strings(item.flags), size_bytes: number(item.size_bytes), signed_content: boolean(item.signed_content), attachments_truncated: boolean(item.attachments_truncated),
    body_content_type: string(item.body_content_type), body_source_content_type: string(item.body_source_content_type), body_projection: string(item.body_projection),
    body_declared_bytes: number(item.body_declared_bytes), body_decoded_bytes_observed: number(item.body_decoded_bytes_observed), body_decoded_bytes: number(item.body_decoded_bytes),
    body_decoded_size_complete: boolean(item.body_decoded_size_complete), body_returned_bytes: number(item.body_returned_bytes), trust: string(item.trust), warning: string(item.warning),
  };
}

export function readMailFolders(value: unknown): MailFolder[] {
  const folders = readMailRecord(value).folders;
  if (!Array.isArray(folders)) return [];
  return folders.flatMap((value: unknown) => {
    const folder = readMailRecord(value);
    return typeof folder.name === "string" && folder.selectable !== false ? [{ ...folder, name: folder.name, display_name: string(folder.display_name), delimiter: string(folder.delimiter),
      attributes: strings(folder.attributes), selectable: boolean(folder.selectable), role: string(folder.role) }] : [];
  });
}

export function readMailSearch(value: unknown) {
  const output = readMailRecord(value);
  return {
    messages: Array.isArray(output.messages) ? output.messages.map(readMailMessage).filter((message): message is MailMessage => message !== null) : [],
    total: Number(output.total || 0), unread: Number(output.unread || 0),
    nextCursor: typeof output.next_cursor === "string" ? output.next_cursor : "",
  };
}

export function readMailSubmissionUnknown(value: unknown, fingerprint: string) {
  const output = readMailRecord(value);
  return output.submission_status === "submission_unknown" ? { messageID: string(output.message_id) || "", fingerprint } : null;
}

function messageReference(item: Record<string, unknown>): MailMessageRef {
  return { folder: string(item.folder), uid: number(item.uid), uidvalidity: number(item.uidvalidity) };
}
function addresses(value: unknown): MailAddress[] | undefined {
  return Array.isArray(value) ? value.map((entry: unknown) => {
    const address = readMailRecord(entry);
    return { ...address, name: string(address.name), address: string(address.address) };
  }) : undefined;
}
function attachments(value: unknown): MailAttachment[] | undefined {
  return Array.isArray(value) ? value.map((entry: unknown) => {
    const item = readMailRecord(entry);
    return { ...item, part_id: string(item.part_id), filename: string(item.filename), content_type: string(item.content_type), declared_size_bytes: number(item.declared_size_bytes),
      decoded_size_bytes: item.decoded_size_bytes === null ? null : number(item.decoded_size_bytes), disposition: string(item.disposition), content_id: string(item.content_id) };
  }) : undefined;
}
function string(value: unknown): string | undefined { return typeof value === "string" ? value : undefined; }
function boolean(value: unknown): boolean | undefined { return typeof value === "boolean" ? value : undefined; }
function number(value: unknown): number | undefined { return typeof value === "number" ? value : undefined; }
function strings(value: unknown): string[] | undefined { return Array.isArray(value) ? value.filter((item: unknown): item is string => typeof item === "string") : undefined; }
