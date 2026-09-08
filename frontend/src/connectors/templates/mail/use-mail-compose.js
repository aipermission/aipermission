import { useEffect, useState } from "react";
import { connectorActionPending, connectorActionRequestID } from "../_shared/action-result";
import { addressValues, replySubject, replyText, submissionDraftFingerprint, unknownSubmissionRetryDecision } from "./helpers";

const emptyCompose = { open: false, reply: false, form: {} };
const emptyRetry = { open: false, fields: null, messageID: "" };

export function useMailCompose({ scopeKey, selectedMessage, outboundPending, runMailAction }) {
  const [compose, setCompose] = useState(emptyCompose);
  const [retryDialog, setRetryDialog] = useState(emptyRetry);

  useEffect(() => {
    setCompose(emptyCompose);
    setRetryDialog(emptyRetry);
  }, [scopeKey]);

  function openCompose() {
    if (outboundPending) return;
    setCompose({ open: true, reply: false, form: {} });
  }

  function openReply() {
    if (outboundPending || !selectedMessage) return;
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

  async function submitMessage(fields, retryConfirmed = false) {
    const actionName = compose.reply ? "reply_message" : "send_message";
    const draftFingerprint = submissionDraftFingerprint(fields);
    const retryDecision = unknownSubmissionRetryDecision(compose.submissionUnknown, fields);
    if (retryDecision.required && !retryConfirmed) {
      setRetryDialog({ open: true, fields, messageID: compose.submissionUnknown.messageID || "", draftChanged: retryDecision.changed });
      return;
    }
    const input = { ...fields };
    if (compose.reply && compose.messageRef) input.message_ref = compose.messageRef;
    try {
      const context = { fields, reply: compose.reply, messageRef: compose.messageRef, draftFingerprint };
      const item = await runMailAction(
        actionName,
        input,
        compose.reply ? "manual Mail workspace reply" : "manual Mail workspace send",
        "sending",
        context,
      );
      if (!item) return;
      if (connectorActionPending(item)) {
        setCompose((current) => ({ ...current, form: fields, pendingRequestID: connectorActionRequestID(item) }));
        return;
      }
      closeAfterSuccess();
    } catch (error) {
      if (error.actionItem?.output?.submission_status === "submission_unknown") {
        setCompose((current) => ({
          ...current,
          submissionUnknown: { messageID: error.actionItem.output.message_id || "", fingerprint: draftFingerprint },
        }));
      }
    }
  }

  function resolvePending(pending, resolution) {
    const { actionName, context } = pending;
    if (actionName !== "send_message" && actionName !== "reply_message") return;
    if (resolution.state === "completed") {
      closeAfterSuccess();
      return;
    }
    const submissionUnknown =
      resolution.item.output?.submission_status === "submission_unknown"
        ? { messageID: resolution.item.output.message_id || "", fingerprint: context.draftFingerprint }
        : null;
    setCompose({ open: true, reply: context.reply, messageRef: context.messageRef, form: context.fields || {}, submissionUnknown });
  }

  function closeAfterSuccess() {
    setCompose(emptyCompose);
    setRetryDialog(emptyRetry);
  }

  return {
    compose,
    retryDialog,
    openCompose,
    openReply,
    submitMessage,
    resolvePending,
    closeCompose: () => setCompose((current) => (current.pendingRequestID ? { ...current, open: false } : emptyCompose)),
    closeRetry: () => setRetryDialog(emptyRetry),
    confirmRetry: () => retryDialog.fields && submitMessage(retryDialog.fields, true),
  };
}
