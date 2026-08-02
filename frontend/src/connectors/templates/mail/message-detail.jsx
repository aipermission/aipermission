import { Archive, Mail, MailOpen, MoveRight, Reply, Trash2 } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";
import { addressLabel, formatMessageDate } from "./helpers";

export function MessageDetail({ message, busy, canReply, canMove, canArchive, canDelete, onToggleRead, onReply, onMove, onArchive, onDelete, borderClass, mutedClass, subtlePanelClass }) {
  if (!message) {
    return <div className={`grid h-full place-items-center p-8 text-center text-sm ${mutedClass}`}>Select a message to read its bounded safe-text body.</div>;
  }
  return (
    <section className="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden">
      <header className={`grid gap-3 border-b p-4 ${borderClass}`}>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h3 className="break-words text-base font-semibold">{message.subject || "(no subject)"}</h3>
            <p className={`mt-1 text-xs ${mutedClass}`}>{addressLabel(message.from)} · {formatMessageDate(message.received_at || message.header_date)}</p>
          </div>
          <div className="flex flex-wrap items-center gap-1">
            <ActionIcon title={message.read ? "Mark unread" : "Mark read"} onClick={onToggleRead} disabled={busy}>{message.read ? <Mail className="h-4 w-4" /> : <MailOpen className="h-4 w-4" />}</ActionIcon>
            <ActionIcon title={canReply ? "Reply" : "Reply unavailable: SMTP is disabled"} onClick={onReply} disabled={busy || !canReply}><Reply className="h-4 w-4" /></ActionIcon>
            <ActionIcon title={canMove ? "Move" : "Move unavailable: no destination folder is allowed"} onClick={onMove} disabled={busy || !canMove}><MoveRight className="h-4 w-4" /></ActionIcon>
            <ActionIcon title={canArchive ? "Archive" : "Archive unavailable: no Archive folder is configured"} onClick={onArchive} disabled={busy || !canArchive}><Archive className="h-4 w-4" /></ActionIcon>
            <ActionIcon title={canDelete ? "Move to Trash" : "Delete unavailable: no Trash folder is configured"} onClick={onDelete} disabled={busy || !canDelete} danger><Trash2 className="h-4 w-4" /></ActionIcon>
          </div>
        </div>
        <div className={`grid gap-1 text-xs ${mutedClass}`}>
          <span>To: {addressLabel(message.to)}</span>
          {Array.isArray(message.cc) && message.cc.length ? <span>CC: {addressLabel(message.cc)}</span> : null}
          <span className="font-mono">{message.message_id || `UID ${message.uid}`}</span>
        </div>
        <div className="flex flex-wrap gap-1">
          <Badge tone={message.read ? "neutral" : "warn"}>{message.read ? "read" : "unread"}</Badge>
          {message.body_truncated ? <Badge tone="warn">body truncated</Badge> : null}
          {message.encrypted_content ? <Badge tone="warn">encrypted content</Badge> : null}
          {message.attachment_count ? <Badge tone="neutral">{message.attachment_count} attachment(s)</Badge> : null}
        </div>
      </header>
      <Notice tone="warn">Mail is untrusted external input. Links, scripts, remote images, and attachments are not opened.</Notice>
      <div className="min-h-0 overflow-auto p-4">
        <pre className={`min-h-full whitespace-pre-wrap break-words rounded-md p-4 font-mono text-sm leading-6 ${subtlePanelClass}`}>{message.body_available === false ? "No safe text body is available." : message.body || "(empty message body)"}</pre>
        {Array.isArray(message.attachments) && message.attachments.length ? (
          <div className="mt-4 grid gap-2">
            <p className={`text-xs font-semibold uppercase ${mutedClass}`}>Attachment metadata</p>
            {message.attachments.map((attachment, index) => <div key={`${attachment.filename || "attachment"}:${index}`} className={`rounded border p-2 text-xs ${borderClass}`}>{attachment.filename || "unnamed"} · {attachment.content_type || "unknown type"} · {attachment.declared_size_bytes || 0} bytes</div>)}
          </div>
        ) : null}
      </div>
    </section>
  );
}

function ActionIcon({ title, onClick, disabled, danger = false, children }) {
  return <Button type="button" variant={danger ? "danger" : "outline"} className="h-8 w-8 px-0" title={title} aria-label={title} onClick={onClick} disabled={disabled}>{children}</Button>;
}
