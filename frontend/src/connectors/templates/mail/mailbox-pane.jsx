import { Inbox, Mail, MailOpen, Search } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Checkbox, Input } from "../../../components/ui/form";
import { addressLabel, formatMessageDate, messageRefKey } from "./helpers";

export function FolderPane({ folders, selectedFolder, folderStats, onSelect, borderClass, mutedClass, rowHoverClass, activeRowClass }) {
  return (
    <section className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden border-r ${borderClass}`}>
      <div className={`border-b p-3 ${borderClass}`}>
        <p className="text-sm font-semibold">Folders</p>
        <p className={`text-xs ${mutedClass}`}>{folders.length} allowed</p>
      </div>
      <div className="min-h-0 overflow-auto p-2">
        {folders.map((folder) => {
          const active = folder.name === selectedFolder;
          const stats = folderStats[folder.name];
          return (
            <button
              type="button"
              key={folder.name}
              className={`flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm ${active ? activeRowClass : rowHoverClass}`}
              onClick={() => onSelect(folder.name)}
            >
              <Inbox className="h-4 w-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{folder.display_name || folder.name}</span>
              {stats?.unread > 0 ? <Badge tone="warn">{stats.unread}</Badge> : null}
            </button>
          );
        })}
        {folders.length === 0 ? <p className={`p-3 text-xs ${mutedClass}`}>No readable folders returned.</p> : null}
      </div>
    </section>
  );
}

export function MessagePane({ messages, selectedRef, query, unreadOnly, hasMore, busy, onQuery, onUnreadOnly, onSearch, onSelect, onLoadMore, borderClass, mutedClass, inputClass, rowHoverClass, activeRowClass }) {
  return (
    <section className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden border-r ${borderClass}`}>
      <form className={`grid gap-2 border-b p-3 ${borderClass}`} onSubmit={onSearch}>
        <div className="relative">
          <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
          <Input className={`pl-9 ${inputClass}`} value={query} onChange={(event) => onQuery(event.target.value)} placeholder="Search subject" disabled={busy} />
        </div>
        <label className={`flex items-center gap-2 text-xs ${mutedClass}`}>
          <Checkbox checked={unreadOnly} onChange={(event) => onUnreadOnly(event.target.checked)} disabled={busy} />
          Unread only
        </label>
      </form>
      <div className="min-h-0 overflow-auto p-2">
        {messages.map((message) => {
          const active = messageRefKey(message) === selectedRef;
          return (
            <button
              type="button"
              key={messageRefKey(message)}
              className={`grid w-full gap-1 rounded-md border-l-2 px-3 py-2 text-left ${active ? activeRowClass : `${message.read ? "border-transparent" : "border-amber-500"} ${rowHoverClass}`}`}
              onClick={() => onSelect(message)}
            >
              <span className={`truncate text-sm ${message.read ? "font-medium" : "font-bold"}`}>{message.subject || "(no subject)"}</span>
              <span className={`truncate text-xs ${mutedClass}`}>{addressLabel(message.from)}</span>
              <span className={`flex items-center justify-between gap-2 text-xs ${mutedClass}`}>
                <span className="truncate">{formatMessageDate(message.received_at || message.header_date)}</span>
                {message.read ? <MailOpen className="h-3.5 w-3.5 shrink-0" /> : <Mail className="h-3.5 w-3.5 shrink-0 text-amber-500" />}
              </span>
            </button>
          );
        })}
        {messages.length === 0 ? <p className={`p-4 text-center text-xs ${mutedClass}`}>{busy ? "Loading messages..." : "No messages match this view."}</p> : null}
      </div>
      <div className={`border-t p-2 ${borderClass}`}>
        <Button type="button" variant="outline" className="h-8 w-full text-xs" onClick={onLoadMore} disabled={!hasMore || busy}>
          {hasMore ? "Load older messages" : "No more messages"}
        </Button>
      </div>
    </section>
  );
}
