import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { CopyButton } from "../ui/copy-button";
import { Dialog } from "../ui/dialog";
import { Textarea } from "../ui/form";
import { Notice } from "../ui/notice";
import { TerminalBlock } from "../ui/terminal-block";

export function ConnectorActionApprovalDialog({ approval, note, action, onNoteChange, onRun, onDecline, onClose }) {
  const requestAge = approval ? formatRequestAge(approval.created_at) : "";
  const requestTimestamp = approval?.created_at ? formatRequestTimestamp(approval.created_at) : "";
  const terminalError = action.state === "failed";
  const stale = action.state === "stale";
  const inputText = approval ? JSON.stringify(approval.input || {}, null, 2) : "";
  const targetLabel = approval ? `${approval.target_name} · ${approval.profile_label}` : "";
  return (
    <Dialog
      open={Boolean(approval)}
      title={approval ? `${approval.connector_kind} action approval` : "Connector approval"}
      description={approval ? `Request #${approval.id} is waiting for your decision${requestAge ? ` · sent ${requestAge}` : ""}.` : ""}
      onClose={onClose}
      size="xl"
      className="max-h-[calc(100vh-96px)]"
      bodyClassName="min-h-0 overflow-hidden p-0"
    >
      {approval ? (
        <div className="grid h-[calc(100vh-196px)] min-h-0 grid-rows-[minmax(0,1fr)_auto]">
          <div className="grid min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] gap-3 p-5">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="warn">pending</Badge>
              {approval.token_name ? <Badge>{approval.token_name}</Badge> : null}
              <Badge>{approval.action_name}</Badge>
              {requestAge ? <Badge title={requestTimestamp}>sent {requestAge}</Badge> : null}
            </div>
            <div className="rounded-md border border-stone-200 bg-stone-50 p-3">
              <p className="text-xs font-semibold uppercase text-stone-500">Target</p>
              <p className="mt-1 text-sm font-semibold text-stone-900">{targetLabel}</p>
              <p className="mt-1 font-mono text-xs text-stone-500">{approval.target_ref}</p>
              {approval.reason ? <p className="mt-2 text-sm text-stone-700">{approval.reason}</p> : null}
            </div>
            <Notice tone="warn" className="py-2 text-xs">
              Review this as a structured connector action. Input, output, notes, and audit records may be persisted in the encrypted local database; redaction is best-effort.
            </Notice>
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-semibold uppercase text-stone-500">Input</span>
                <CopyButton value={inputText} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" />
              </div>
              <TerminalBlock>{inputText}</TerminalBlock>
            </div>
          </div>
          <div className="grid gap-3 border-t border-stone-200 bg-white p-5 shadow-[0_-8px_18px_rgba(15,23,42,0.06)]">
            <label className="grid gap-2 text-sm font-medium text-stone-800">
              Decline note
              <Textarea
                value={note}
                onChange={(event) => onNoteChange(event.target.value)}
                placeholder="Optional. Decline stores this note as guidance for the AI."
                rows={2}
                className="!min-h-16 resize-none"
              />
            </label>
            {action.state === "error" || stale || terminalError ? <Notice tone="bad">{action.error}</Notice> : null}
            {stale || terminalError ? (
              <Button type="button" onClick={onClose}>
                OK
              </Button>
            ) : (
              <div className="grid grid-cols-2 gap-2">
                <Button type="button" variant="outline" onClick={onDecline} disabled={action.state !== "idle" && action.state !== "error"}>
                  {action.state === "declining" ? "Declining..." : "Decline"}
                </Button>
                <Button type="button" onClick={onRun} disabled={action.state !== "idle" && action.state !== "error"}>
                  {action.state === "running" ? "Running..." : "Run"}
                </Button>
              </div>
            )}
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}

function formatRequestAge(value) {
  if (!value) return "";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function formatRequestTimestamp(value) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  return new Date(timestamp).toLocaleString();
}
