import { useEffect, useRef } from "react";
import { RefreshCcw, Send } from "lucide-react";
import { Button } from "../ui/button";
import { Drawer } from "../ui/drawer";
import { Select, Textarea } from "../ui/form";
import { Notice } from "../ui/notice";

export function MessagesDialog({ open, target, tokens, tokenID, state, text, onTokenChange, onTextChange, onSubmit, onRefresh, onClose }) {
  const messageListRef = useRef(null);
  const filteredMessages = (tokenID ? state.data.filter((message) => Number(message.token_id) === Number(tokenID)) : state.data)
    .slice()
    .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime() || Number(a.id) - Number(b.id));

  useEffect(() => {
    if (!open || !messageListRef.current) return;
    messageListRef.current.scrollTop = messageListRef.current.scrollHeight;
  }, [open, tokenID, filteredMessages.length]);

  return (
    <Drawer
      open={open}
      title={target ? `${target.name} messages` : "Messages"}
      description="Send a note to the AI through the next MCP response, or read notes sent by the AI."
      onClose={onClose}
      bodyClassName="overflow-hidden"
    >
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-2">
          <Select value={tokenID} onChange={(event) => onTokenChange(event.target.value)} className="h-9">
            <option value="" disabled>
              Select token
            </option>
            {tokens.map((token) => (
              <option key={token.id} value={token.id}>
                {token.name}
              </option>
            ))}
          </Select>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh messages" onClick={onRefresh}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
          {tokens.length === 0 ? <Notice className="col-span-2">No token has access to this target.</Notice> : null}
        </div>
        <div ref={messageListRef} className="min-h-0 overflow-auto pr-1">
          {filteredMessages.length === 0 ? (
            <div className="grid h-full min-h-32 place-items-center text-center">
              <p className="text-sm text-stone-500">No messages for this token yet.</p>
            </div>
          ) : (
            <div className="flex min-h-full flex-col justify-end gap-2">
              {filteredMessages.map((message) => (
                <div
                  key={message.id}
                  className={`flex ${message.direction === "ai_to_user" ? "justify-start" : "justify-end"}`}
                >
                  <div className="w-fit max-w-[70%] rounded-lg border border-stone-200 bg-white px-3 py-2 shadow-sm">
                    <div className="mb-1 text-[11px] text-stone-500">
                      {message.direction === "ai_to_user" ? "AI" : "You"} · {formatMessageTime(message.created_at)}
                    </div>
                    <p className="whitespace-pre-wrap break-words text-sm text-stone-800">{message.message}</p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
        <form className="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-2 border-t border-stone-200 pt-2" onSubmit={onSubmit}>
          <label className="grid gap-1 text-sm font-medium text-stone-800">
            <Textarea
              value={text}
              onChange={(event) => onTextChange(event.target.value)}
              placeholder="Note to send to the AI in the next MCP response..."
              rows={1}
              className="h-10 !min-h-10 max-h-24 resize-none overflow-y-auto py-2"
            />
          </label>
          <Button type="submit" className="h-10 px-3" disabled={!tokenID || !text.trim() || state.state === "sending"}>
            <Send className="h-4 w-4" />
            Send
          </Button>
          {state.state === "error" ? <Notice tone="bad" className="col-span-2">{state.error}</Notice> : null}
        </form>
      </div>
    </Drawer>
  );
}

function isUnreadMessage(message) {
  return message.direction === "ai_to_user" && !message.consumed_at;
}

function formatMessageTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
