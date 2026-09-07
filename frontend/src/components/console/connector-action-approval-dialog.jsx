import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { CopyButton } from "../ui/copy-button";
import { Dialog } from "../ui/dialog";
import { Textarea } from "../ui/form";
import { Notice } from "../ui/notice";
import { TerminalBlock } from "../ui/terminal-block";
import { formatLocalTimestamp, formatRelativeAge } from "../../lib/date-time";

export function ConnectorActionApprovalDialog({ approval, note, action, onNoteChange, onRun, onDecline, onClose }) {
  const requestAge = approval ? formatRelativeAge(approval.created_at) : "";
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
        <ConnectorApprovalContent
          approval={approval}
          requestAge={requestAge}
          note={note}
          action={action}
          onNoteChange={onNoteChange}
          onRun={onRun}
          onDecline={onDecline}
          onClose={onClose}
        />
      ) : null}
    </Dialog>
  );
}

function ConnectorApprovalContent({ approval, requestAge, note, action, onNoteChange, onRun, onDecline, onClose }) {
  return (
    <div className="grid h-[calc(100vh-196px)] min-h-0 grid-rows-[minmax(0,1fr)_auto]">
      <div className="grid min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] gap-3 p-5">
        <ApprovalContext approval={approval} requestAge={requestAge} />
        <Notice tone="warn" className="py-2 text-xs">
          Review this as a structured connector action. Input, output, notes, and audit records may be persisted in the encrypted local
          database; redaction is best-effort.
        </Notice>
        <ApprovalPayloads approval={approval} loading={action.state === "loading"} />
      </div>
      <ApprovalDecisionFooter
        action={action}
        note={note}
        onNoteChange={onNoteChange}
        onRun={onRun}
        onDecline={onDecline}
        onClose={onClose}
      />
    </div>
  );
}

function ApprovalContext({ approval, requestAge }) {
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone="warn">pending</Badge>
        {approval.token_name ? <Badge>{approval.token_name}</Badge> : null}
        <Badge>{approval.action_name}</Badge>
        {requestAge ? <Badge title={formatLocalTimestamp(approval.created_at)}>sent {requestAge}</Badge> : null}
      </div>
      <div className="rounded-md border border-stone-200 bg-stone-50 p-3">
        <p className="text-xs font-semibold uppercase text-stone-500">Target</p>
        <p className="mt-1 text-sm font-semibold text-stone-900">{`${approval.target_name} · ${approval.profile_label}`}</p>
        <p className="mt-1 font-mono text-xs text-stone-500">{approval.target_ref}</p>
        {approval.title ? <p className="mt-3 text-sm font-semibold text-stone-900">{approval.title}</p> : null}
        {approval.summary ? <p className="mt-1 text-sm text-stone-700">{approval.summary}</p> : null}
        {approval.reason ? <p className="mt-2 text-sm text-stone-700">{approval.reason}</p> : null}
      </div>
    </>
  );
}

function ApprovalPayloads({ approval, loading }) {
  const input = JSON.stringify(approval.input || {}, null, 2);
  const preview = JSON.stringify(approval.preview || {}, null, 2);
  return (
    <div className="grid min-h-0 grid-cols-1 gap-3 overflow-hidden lg:grid-cols-2">
      <ApprovalPayload
        title="Approval preview"
        value={preview}
        loading={loading}
        loadingText="Loading the exact approval preview..."
        empty={Object.keys(approval.preview || {}).length === 0}
      />
      <ApprovalPayload title="Redacted input" value={input} loading={loading} />
    </div>
  );
}

function ApprovalPayload({ title, value, loading, loadingText = `Loading ${title.toLowerCase()}...`, empty = false }) {
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-semibold uppercase text-stone-500">{title}</span>
        <CopyButton value={value} variant="outline" className="h-8 px-2 text-xs" iconClassName="h-3.5 w-3.5" />
      </div>
      {loading ? (
        <Notice>{loadingText}</Notice>
      ) : empty ? (
        <Notice>No structured preview was provided.</Notice>
      ) : (
        <TerminalBlock>{value}</TerminalBlock>
      )}
    </div>
  );
}

function ApprovalDecisionFooter({ action, note, onNoteChange, onRun, onDecline, onClose }) {
  const terminal = action.state === "stale" || action.state === "failed" || action.state === "load_error";
  const decisionDisabled = action.state !== "idle" && action.state !== "error";
  return (
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
      {action.state === "error" || terminal ? <Notice tone="bad">{action.error}</Notice> : null}
      {terminal ? (
        <Button type="button" onClick={onClose}>
          OK
        </Button>
      ) : (
        <div className="grid grid-cols-2 gap-2">
          <Button type="button" variant="outline" onClick={onDecline} disabled={decisionDisabled}>
            {action.state === "declining" ? "Declining..." : "Decline"}
          </Button>
          <Button type="button" onClick={onRun} disabled={decisionDisabled}>
            {action.state === "running" ? "Running..." : "Run"}
          </Button>
        </div>
      )}
    </div>
  );
}
