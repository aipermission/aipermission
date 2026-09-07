import { ChevronRight, CircleCheck, Mail, PenLine, RefreshCcw } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { ConnectorEndpointFooter } from "../_shared/endpoint-footer";
import { StructuredSessionEmpty } from "../_shared/structured-session-empty";
import { MailActionResultDialog } from "./action-result-dialog";
import { ComposeDialog } from "./compose-dialog";
import { messageRefKey } from "./helpers";
import { FolderPane, MessagePane } from "./mailbox-pane";
import { MessageDetail } from "./message-detail";
import { DeleteMessageDialog, MoveMessageDialog, RetryUnknownSubmissionDialog } from "./message-dialogs";
import { targetEndpoint } from "./model";
import { useMailWorkspace } from "./use-mail-workspace";

export function MailConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const workspace = useMailWorkspace({ target, approvals, session, onRefreshActivity });
  const { runner, mailbox, compose } = workspace;
  const styles = connectorConsoleTheme(theme);
  const activeRowClass =
    theme === "light" ? "border-emerald-300 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100";
  const resultClass =
    theme === "light" ? "border-emerald-200 bg-emerald-50 text-emerald-900" : "border-emerald-800 bg-emerald-950/40 text-emerald-100";

  if (!workspace.activeSession.active) {
    return (
      <StructuredSessionEmpty
        icon={Mail}
        title="No active Mail session"
        description="Start a structured session to browse bounded IMAP content and submit guarded SMTP actions."
        buttonLabel="Start Mail session"
        onStart={onNewStructuredSession}
        panelClass={styles.panel}
        mutedClass={styles.muted}
        footer={<MailEndpointFooter target={target} borderClass={styles.border} mutedClass={styles.muted} />}
      />
    );
  }

  const composeError = ["send_message", "reply_message"].includes(runner.state.result?.actionName) ? runner.state.error : "";
  return (
    <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] ${styles.panel}`}>
      <MailToolbar
        target={target}
        latestAction={runner.latestAction}
        state={runner.state}
        resultClass={resultClass}
        imapEnabled={workspace.imapEnabled}
        smtpEnabled={workspace.smtpEnabled}
        busy={workspace.busy}
        outboundPending={workspace.outboundPending}
        onResult={() => runner.state.result && runner.openResultDialog(runner.state.result)}
        onRefresh={mailbox.refreshMailbox}
        onCompose={workspace.openCompose}
        borderClass={styles.border}
      />
      {workspace.imapEnabled ? (
        <div className="grid min-h-0 gap-2 overflow-y-auto xl:grid-cols-[220px_340px_minmax(0,1fr)] xl:gap-0 xl:overflow-hidden [&>*]:min-h-[320px] xl:[&>*]:min-h-0">
          <FolderPane
            folders={mailbox.folders}
            selectedFolder={mailbox.selectedFolder}
            folderStats={mailbox.folderStats}
            onSelect={mailbox.selectFolder}
            borderClass={styles.border}
            mutedClass={styles.muted}
            rowHoverClass={styles.rowHover}
            activeRowClass={activeRowClass}
          />
          <MessagePane
            messages={mailbox.messages}
            selectedRef={messageRefKey(mailbox.selectedMessage)}
            query={mailbox.query}
            unreadOnly={mailbox.unreadOnly}
            hasMore={Boolean(mailbox.nextCursor)}
            busy={workspace.busy}
            onQuery={mailbox.setQuery}
            onUnreadOnly={mailbox.setUnreadFilter}
            onSearch={(event) => {
              event.preventDefault();
              if (!workspace.busy) void mailbox.search();
            }}
            onSelect={mailbox.selectMessage}
            onLoadMore={() => mailbox.loadMessages(mailbox.selectedFolder, { reset: false, cursor: mailbox.nextCursor })}
            borderClass={styles.border}
            mutedClass={styles.muted}
            inputClass={styles.input}
            rowHoverClass={styles.rowHover}
            activeRowClass={activeRowClass}
          />
          <MessageDetail
            message={mailbox.selectedMessage}
            busy={workspace.busy}
            canReply={workspace.smtpEnabled && !workspace.outboundPending}
            canMove={workspace.destinationFolders.length > 0}
            canArchive={workspace.canArchive}
            canDelete={workspace.canDelete}
            onToggleRead={mailbox.toggleRead}
            onReply={workspace.openReply}
            onMove={mailbox.openMove}
            onArchive={() => mailbox.moveSelected("archive_message")}
            onDelete={mailbox.openDelete}
            borderClass={styles.border}
            mutedClass={styles.muted}
            subtlePanelClass={styles.subtlePanel}
          />
        </div>
      ) : (
        <SMTPOnlyPanel
          busy={workspace.busy}
          outboundPending={workspace.outboundPending}
          smtpEnabled={workspace.smtpEnabled}
          onCompose={workspace.openCompose}
          borderClass={styles.border}
          mutedClass={styles.muted}
          subtlePanelClass={styles.subtlePanel}
        />
      )}
      <div className={`grid gap-2 border-t px-3 py-2 ${styles.border}`}>
        {runner.state.error ? <Notice tone="bad">{runner.state.error}</Notice> : null}
        <MailEndpointFooter target={target} mutedClass={styles.muted} />
      </div>
      <ComposeDialog
        draft={compose.compose}
        busy={runner.state.state === "sending" || Boolean(compose.compose.pendingRequestID)}
        error={compose.compose.open ? composeError : ""}
        onClose={compose.closeCompose}
        onSubmit={compose.submitMessage}
      />
      <MailActionResultDialog value={runner.resultDialog} onClose={runner.closeResultDialog} />
      <MoveMessageDialog
        dialog={mailbox.moveDialog}
        folders={workspace.destinationFolders}
        busy={runner.state.state === "updating"}
        onClose={mailbox.closeMove}
        onDestination={mailbox.setMoveDestination}
        onConfirm={() => mailbox.moveSelected("move_message", mailbox.moveDialog.destination)}
      />
      <DeleteMessageDialog
        open={mailbox.deleteOpen}
        trashFolder={target.public?.trash_folder}
        busy={runner.state.state === "updating"}
        onClose={mailbox.closeDelete}
        onConfirm={() => mailbox.moveSelected("delete_message")}
      />
      <RetryUnknownSubmissionDialog
        value={compose.retryDialog}
        busy={runner.state.state === "sending"}
        onClose={compose.closeRetry}
        onConfirm={compose.confirmRetry}
      />
    </div>
  );
}

function MailToolbar(props) {
  const { target, latestAction, state, resultClass, imapEnabled, smtpEnabled, busy, outboundPending } = props;
  return (
    <div className={`flex min-w-0 items-center justify-between gap-3 border-b px-3 py-2 ${props.borderClass}`}>
      <div className="flex min-w-0 items-center gap-2">
        <span className="truncate text-sm font-semibold">{target.public?.mailbox_address || target.profile_label}</span>
        {latestAction ? (
          <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
            {latestAction.action_name}
          </Badge>
        ) : null}
        {state.message ? (
          <button
            type="button"
            className={`flex h-7 max-w-72 items-center gap-1.5 overflow-hidden rounded-md border px-2 text-left text-xs ${resultClass}`}
            onClick={props.onResult}
            title={state.message}
          >
            <CircleCheck className="h-3.5 w-3.5 shrink-0" />
            <span className="min-w-0 flex-1 truncate">{state.message}</span>
            <ChevronRight className="h-3.5 w-3.5 shrink-0" />
          </button>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        {imapEnabled ? (
          <Button
            type="button"
            variant="outline"
            className="h-8 w-8 px-0"
            title="Refresh mailbox"
            aria-label="Refresh mailbox"
            onClick={() => props.onRefresh()}
            disabled={busy}
          >
            <RefreshCcw className={`h-4 w-4 ${state.state === "loading" ? "animate-spin" : ""}`} />
          </Button>
        ) : null}
        <Button type="button" className="h-8" onClick={props.onCompose} disabled={busy || outboundPending || !smtpEnabled}>
          <PenLine className="h-4 w-4" />
          Compose
        </Button>
      </div>
    </div>
  );
}

function SMTPOnlyPanel({ busy, outboundPending, smtpEnabled, onCompose, borderClass, mutedClass, subtlePanelClass }) {
  return (
    <div className="grid min-h-0 place-items-center p-8 text-center">
      <div className={`grid max-w-lg gap-3 rounded-md border p-6 ${borderClass} ${subtlePanelClass}`}>
        <PenLine className={`mx-auto h-9 w-9 ${mutedClass}`} />
        <h3 className="text-base font-semibold">SMTP-only Mail profile</h3>
        <p className={`text-sm ${mutedClass}`}>
          Mailbox browsing is disabled for this profile. Compose remains available through the configured SMTP connection.
        </p>
        <Button type="button" className="mx-auto" onClick={onCompose} disabled={busy || outboundPending || !smtpEnabled}>
          <PenLine className="h-4 w-4" />
          Compose message
        </Button>
      </div>
    </div>
  );
}

function MailEndpointFooter({ target, borderClass = "", mutedClass }) {
  return (
    <ConnectorEndpointFooter leading={target.ref} trailing={targetEndpoint({ target })} borderClass={borderClass} mutedClass={mutedClass} />
  );
}
