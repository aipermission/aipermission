import { useEffect, useRef, useState } from "react";
import { connectorActionError, connectorActionPending, connectorActionRequestID } from "../_shared/action-result";
import {
  addressValues,
  mailActionSummary,
  replySubject,
  replyText,
  submissionDraftFingerprint,
  unknownSubmissionRetryDecision,
} from "./helpers";
import { errorMessage } from "../../../lib/errors";
import { MailActionFailure } from "./use-mail-action-runner";
import { readMailSubmissionUnknown } from "./message-output";
import type { MailComposeDraft, MailMessage, MailSubmittedFields } from "./message-types";
import type { MailRetryDialog } from "./message-dialogs";
import type { MailActionItem, MailActionResolution, MailPendingAction, MailPendingContext, RunMailAction } from "./action-types";
import type { MailActionResult } from "./action-result-dialog";

interface MailComposeProps {
  scopeKey: string;
  selectedMessage: MailMessage | null;
  outboundPending: boolean;
  runMailAction: RunMailAction;
}
type RetryState = MailRetryDialog & { fields: MailSubmittedFields | null };
export type MailComposeOutcome = MailActionResult & { error: string };

const emptyCompose: MailComposeDraft = { open: false, reply: false, form: {} };
const emptyRetry: RetryState = { open: false, fields: null, messageID: "" };

export function useMailCompose({ scopeKey, selectedMessage, outboundPending, runMailAction }: MailComposeProps) {
  const [compose, setCompose] = useState(emptyCompose);
  const [retryDialog, setRetryDialog] = useState(emptyRetry);
  const [outcome, setOutcome] = useState<MailComposeOutcome | null>(null);
  // Mailbox reads cannot replace the latest outbound submission's presentation.
  const submissionOwner = useRef<MailPendingContext | null>(null);

  useEffect(() => {
    submissionOwner.current = null;
    setCompose(emptyCompose);
    setRetryDialog(emptyRetry);
    setOutcome(null);
    return () => {
      submissionOwner.current = null;
    };
  }, [scopeKey]);

  function openCompose() {
    if (outboundPending) return;
    submissionOwner.current = null;
    setOutcome(null);
    setCompose({ open: true, reply: false, form: {} });
  }

  function openReply() {
    if (outboundPending || !selectedMessage) return;
    submissionOwner.current = null;
    setOutcome(null);
    setCompose({
      open: true,
      reply: true,
      messageRef: selectedMessage.message_ref,
      form: {
        to: addressValues(selectedMessage.reply_to?.length ? selectedMessage.reply_to : selectedMessage.from).join(", "),
        subject: replySubject(selectedMessage.subject),
        text_body: replyText(selectedMessage),
        html_body: "",
      },
    });
  }

  async function submitMessage(fields: MailSubmittedFields, retryConfirmed = false) {
    const actionName = compose.reply ? "reply_message" : "send_message";
    const draftFingerprint = submissionDraftFingerprint(fields);
    const retryDecision = unknownSubmissionRetryDecision(compose.submissionUnknown, fields);
    if (retryDecision.required && !retryConfirmed) {
      setRetryDialog({ open: true, fields, messageID: compose.submissionUnknown?.messageID || "", draftChanged: retryDecision.changed });
      return;
    }
    const input: Record<string, unknown> = { ...fields };
    if (compose.reply && compose.messageRef) input.message_ref = compose.messageRef;
    const context = { fields, reply: compose.reply, messageRef: compose.messageRef, draftFingerprint };
    submissionOwner.current = context;
    setOutcome(null);
    try {
      const item = await runMailAction(
        actionName,
        input,
        compose.reply ? "manual Mail workspace reply" : "manual Mail workspace send",
        "sending",
        context,
      );
      if (!item || submissionOwner.current !== context) return;
      if (connectorActionPending(item)) {
        setCompose((current) => ({ ...current, form: fields, pendingRequestID: connectorActionRequestID(item) }));
        return;
      }
      showOutcome(actionName, item);
      closeAfterSuccess();
    } catch (error) {
      if (submissionOwner.current !== context) return;
      showOutcome(actionName, error instanceof MailActionFailure ? error.actionItem : null, errorMessage(error, "Mail action failed."));
      const submissionUnknown =
        error instanceof MailActionFailure ? readMailSubmissionUnknown(error.actionItem.output, draftFingerprint) : null;
      if (submissionUnknown) {
        setCompose((current) => ({
          ...current,
          submissionUnknown,
        }));
      }
    }
  }

  function resolvePending(pending: MailPendingAction, resolution: MailActionResolution) {
    const { actionName, context } = pending;
    if ((actionName !== "send_message" && actionName !== "reply_message") || submissionOwner.current !== context) return;
    const error = connectorActionError(resolution.item, "Mail submission was not approved or could not be completed.");
    showOutcome(actionName, resolution.item, error);
    if (resolution.state === "completed") {
      closeAfterSuccess();
      return;
    }
    const submissionUnknown = readMailSubmissionUnknown(resolution.item.output, context.draftFingerprint || "");
    setCompose({
      open: true,
      reply: Boolean(context.reply),
      messageRef: context.messageRef,
      form: context.fields || {},
      submissionUnknown,
    });
  }

  function showOutcome(actionName: string, item: MailActionItem | null, error = "") {
    setOutcome({ actionName, item, error, summary: error || mailActionSummary(actionName, item) });
  }

  function closeAfterSuccess() {
    setCompose(emptyCompose);
    setRetryDialog(emptyRetry);
  }

  return {
    compose,
    retryDialog,
    outcome,
    openCompose,
    openReply,
    submitMessage,
    resolvePending,
    closeCompose: () => setCompose((current) => (current.pendingRequestID ? { ...current, open: false } : emptyCompose)),
    closeRetry: () => setRetryDialog(emptyRetry),
    confirmRetry: () => retryDialog.fields && submitMessage(retryDialog.fields, true),
  };
}
