import { useMemo } from "react";
import { mailFolderAllowed, mailProtocolCapabilities } from "./helpers";
import { useMailActionRunner } from "./use-mail-action-runner";
import { useMailCompose } from "./use-mail-compose";
import { useMailMailbox } from "./use-mailbox";

const outboundActions = new Set(["send_message", "reply_message"]);

export function useMailWorkspace({ target, approvals, session, onRefreshActivity }) {
  const activeSession = session || { active: false, startedAt: "" };
  const scopeKey = `${target.ref}:${activeSession.active ? activeSession.startedAt || "active" : "inactive"}`;
  let mailbox;
  let compose;

  async function resolvePending(pending, resolution) {
    if (outboundActions.has(pending.actionName)) compose.resolvePending(pending, resolution);
    else await mailbox.resolvePending(pending, resolution);
  }

  const runner = useMailActionRunner({ target, approvals, scopeKey, onRefreshActivity, onResolution: resolvePending });
  const busy = runner.state.state !== "idle" && runner.state.state !== "error";
  const outboundPending =
    Object.values(runner.pendingActions).some((pending) => outboundActions.has(pending.actionName)) ||
    runner.activeItems.some((item) => outboundActions.has(item.action_name) && ["approval_pending", "running"].includes(item.status));
  const capabilities = mailProtocolCapabilities(target.public);

  mailbox = useMailMailbox({
    scopeKey,
    activeSession,
    imapEnabled: capabilities.imapEnabled,
    busy,
    folderSelectionLocked: runner.state.state === "sending" || runner.state.state === "updating",
    runMailAction: runner.runMailAction,
  });
  compose = useMailCompose({
    scopeKey,
    selectedMessage: mailbox.selectedMessage,
    outboundPending,
    runMailAction: runner.runMailAction,
  });

  const destinationFolders = useMemo(() => allowedDestinationFolders(target), [target]);
  function clearActionError() {
    runner.setState((current) => ({ ...current, error: "" }));
  }

  return {
    activeSession,
    ...capabilities,
    busy,
    outboundPending,
    destinationFolders,
    canArchive: mailFolderAllowed(target.public?.archive_folder, target.public?.allowed_mutation_destination_folders),
    canDelete: mailFolderAllowed(target.public?.trash_folder, target.public?.allowed_mutation_destination_folders),
    runner,
    mailbox,
    compose,
    openCompose() {
      clearActionError();
      compose.openCompose();
    },
    openReply() {
      clearActionError();
      compose.openReply();
    },
  };
}

function allowedDestinationFolders(target) {
  const allowed = Array.isArray(target.public?.allowed_mutation_destination_folders)
    ? target.public.allowed_mutation_destination_folders
    : [];
  return allowed.map((name) => ({ name, display_name: name, selectable: true }));
}
