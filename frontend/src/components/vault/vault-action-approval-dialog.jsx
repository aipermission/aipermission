import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { CopyButton } from "../ui/copy-button";
import { Dialog } from "../ui/dialog";
import { Textarea } from "../ui/form";
import { Notice } from "../ui/notice";
import { TerminalBlock } from "../ui/terminal-block";
import { formatLocalTimestamp, formatRelativeAge, formatRelativeDeadline } from "../../lib/date-time";

export function VaultActionApprovalDialog({ approval, note, action, onNoteChange, onRun, onDecline, onClose }) {
  const input = approval ? JSON.stringify(approval.input || {}, null, 2) : "";
  const age = approval ? formatRelativeAge(approval.created_at) : "";
  const timestamp = approval ? formatLocalTimestamp(approval.created_at) : "";
  const expiry = approval ? formatRelativeDeadline(approval.expires_at) : "";
  const expiryTimestamp = approval ? formatLocalTimestamp(approval.expires_at) : "";
  const terminal = action.state === "stale" || action.state === "failed";
  const context = approval?.approval_context || {};
  const approvedItems = context.items || [];
  const appliesSessionEnvironment = approvedItems.length > 0;

  return (
    <Dialog
      open={Boolean(approval)}
      title="Vault action approval"
      description={approval ? `Request #${approval.id}${age ? ` · sent ${age}` : ""}` : ""}
      onClose={onClose}
      size="xl"
      className="max-h-[calc(100vh-96px)]"
      bodyClassName="min-h-0 overflow-hidden p-0"
    >
      {approval ? (
        <div className="grid h-[calc(100vh-196px)] min-h-0 grid-rows-[minmax(0,1fr)_auto]">
          <div className={`grid min-h-0 gap-3 p-5 ${approvedItems.length > 0 ? "grid-rows-[auto_auto_auto_auto_minmax(0,1fr)]" : "grid-rows-[auto_auto_auto_minmax(0,1fr)]"}`}>
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="warn">pending</Badge>
              <Badge>{approval.token_name}</Badge>
              <Badge>{approval.project_name}</Badge>
              <Badge>{approval.action_name}</Badge>
              {age ? <Badge title={timestamp}>sent {age}</Badge> : null}
              {expiry ? <Badge tone="warn" title={expiryTimestamp}>{expiry}</Badge> : null}
            </div>
            <div className="rounded-md border border-stone-200 bg-stone-50 p-3">
              <p className="text-xs font-semibold uppercase text-stone-500">Reason</p>
              <p className="mt-1 text-sm text-stone-800">{approval.reason || "No reason supplied."}</p>
              {context.target_id ? (
                <p className="mt-2 font-mono text-xs text-stone-500">
                  {context.connector_kind} target {context.target_id} · profile {context.profile_id} · session {context.expected_session_id || "new"}
                </p>
              ) : null}
            </div>
            <Notice tone="warn" className="py-2 text-xs">
              {appliesSessionEnvironment
                ? "Running this approval delivers the listed values to the remote session. Every process in that shell can read, transform, persist, or transmit them. Exact-value redaction only reduces accidental output exposure, and detached processes may retain inherited values."
                : "Running this approval generates a new value inside the local encrypted Vault. The generated value stays hidden from the AI and this dialog."}
            </Notice>
            {approvedItems.length > 0 ? (
              <div className="grid gap-1 rounded-md border border-stone-200 bg-white p-3">
                <p className="text-xs font-semibold uppercase text-stone-500">Environment assignments</p>
                {approvedItems.map((item) => (
                  <div key={`${item.source_project_id}:${item.item_id}`} className="flex items-center justify-between gap-3 text-sm">
                    <span className="font-mono font-semibold text-stone-900">{item.name}</span>
                    <span className="text-xs text-stone-500">
                      project {item.source_project_id}{item.replace_existing ? " · overwrites existing shell value" : ""}
                    </span>
                  </div>
                ))}
              </div>
            ) : null}
            <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-semibold uppercase text-stone-500">Requested metadata</span>
                <CopyButton value={input} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" />
              </div>
              <TerminalBlock>{input}</TerminalBlock>
            </div>
          </div>
          <div className="grid gap-3 border-t border-stone-200 bg-white p-5 shadow-[0_-8px_18px_rgba(15,23,42,0.06)]">
            <label className="grid gap-2 text-sm font-medium text-stone-800">
              Decision note
              <Textarea
                value={note}
                onChange={(event) => onNoteChange(event.target.value)}
                placeholder="Optional guidance for the AI."
                rows={2}
                className="!min-h-16 resize-none"
              />
            </label>
            {action.error ? <Notice tone="bad">{action.error}</Notice> : null}
            {terminal ? (
              <Button type="button" onClick={onClose}>OK</Button>
            ) : (
              <div className="grid grid-cols-2 gap-2">
                <Button type="button" variant="outline" onClick={onDecline} disabled={!["idle", "error"].includes(action.state)}>
                  {action.state === "declining" ? "Declining..." : "Decline"}
                </Button>
                <Button type="button" onClick={onRun} disabled={!["idle", "error"].includes(action.state)}>
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
