export interface MailMessageRef {
  folder?: string;
  uidvalidity?: number;
  uid?: number;
}
export interface MailAddress {
  name?: string;
  address?: string;
}
export interface MailAttachment {
  part_id?: string;
  filename?: string;
  content_type?: string;
  declared_size_bytes?: number;
  decoded_size_bytes?: number | null;
  disposition?: string;
  content_id?: string;
}
export interface MailMessage extends MailMessageRef {
  message_ref?: MailMessageRef;
  subject?: string;
  from?: MailAddress[];
  to?: MailAddress[];
  cc?: MailAddress[];
  reply_to?: MailAddress[];
  received_at?: string;
  header_date?: string;
  message_id?: string;
  read?: boolean;
  body?: string;
  body_available?: boolean;
  body_truncated?: boolean;
  encrypted_content?: boolean;
  attachment_count?: number;
  attachments?: MailAttachment[];
  flags?: string[];
  size_bytes?: number;
  signed_content?: boolean;
  attachments_truncated?: boolean;
  body_content_type?: string;
  body_source_content_type?: string;
  body_projection?: string;
  body_declared_bytes?: number;
  body_decoded_bytes_observed?: number;
  body_decoded_bytes?: number;
  body_decoded_size_complete?: boolean;
  body_returned_bytes?: number;
  trust?: string;
  warning?: string;
}
export interface MailDraftFields {
  to?: string | string[];
  cc?: string | string[];
  bcc?: string | string[];
  subject?: string;
  text_body?: string;
  html_body?: string;
}
export interface MailComposeDraft {
  open: boolean;
  reply: boolean;
  form: MailDraftFields;
  messageRef?: MailMessageRef;
  pendingRequestID?: number;
  submissionUnknown?: { messageID: string; fingerprint: string } | null;
}
export type MailSubmittedFields = Required<Omit<MailDraftFields, "to" | "cc" | "bcc">> & { to: string[]; cc: string[]; bcc: string[] };
export interface MailFolder {
  name: string;
  display_name?: string;
  delimiter?: string;
  attributes?: string[];
  selectable?: boolean;
  role?: string;
}
export interface MailFolderStats {
  unread: number;
  total?: number;
}
