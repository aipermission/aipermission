import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import {
  currentConnectorTargetProfilePermissions,
  effectiveConnectorTargetProfilePermissions,
  selectedConnectorProfileID,
} from "../lib/connector-permissions";
import { useGateway } from "../lib/gateway-context";
import { effectiveRule, permissionLifetimeLabel } from "../lib/permissions";
import { useConnectorPermissions } from "../lib/use-connector-permissions";
import { ConnectorActionApprovalDialog } from "../components/console/connector-action-approval-dialog";
import { ConnectorActivityDialog } from "../components/console/connector-activity-dialog";
import { ConsoleTargetSidebar, recoverableRunningActions, targetUsesLiveConsole } from "../components/console/console-target-sidebar";
import { ConsoleWorkspacePanel } from "../components/console/console-workspace-panel";
import { MessagesDialog } from "../components/console/messages-dialog";
import { TokenPermissionPanel } from "../components/console/token-permission-panel";
import { useConsolePageState } from "../components/console/use-console-page-state";
import { useConsoleMessages } from "../components/console/use-console-messages";
import { useConsoleTargetSelection } from "../components/console/use-console-target-selection";
import { useConsoleWorkspaceSession } from "../components/console/use-console-workspace-session";
import { useConnectorApprovalDialog } from "../components/console/use-connector-approval-dialog";
import { getConnectorTemplate } from "../connectors/templates/registry";

export function ConsolePage() {
  const {
    liveConsoleTargets,
    targets,
    tokens,
    connectorActionApprovals,
    messages,
    loadConsoleSessions,
    loadTokens,
    loadTargets,
    loadConnectorActionApprovals,
    loadMessages,
    markRuntimeMessagesRead,
    consoleSessions,
    newConsoleSession,
    attachConsoleSession,
    closeConsoleSession,
    cancelConsoleCommand,
    restartConsoleSession,
    sendConsoleInput,
    resizeConsoleSession,
    runConnectorActionApproval,
    declineConnectorActionApproval,
    mcpRuntime,
    theme,
  } = useGateway();
  const [searchParams, setSearchParams] = useSearchParams();
  const { connectorPermissionState, loadAllConnectorPermissions, loadConnectorActions, replaceTokenConnectorPermissions } =
    useConnectorPermissions(tokens.data);
  const [targetsCompact, setTargetsCompact] = useState(false);
  const [tokensCompact, setTokensCompact] = useState(false);
  const [connectorActivityOpen, setConnectorActivityOpen] = useState(false);
  const [connectorOperation, setConnectorOperation] = useState({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  const [now, setNow] = useState(Date.now());

  const selectedTargetRef = searchParams.get("target");
  const sessions = consoleSessions.data || [];
  const approvalDialog = useConnectorApprovalDialog({
    approvals: connectorActionApprovals?.data,
    selectedTargetRef,
    runApproval: runConnectorActionApproval,
    declineApproval: declineConnectorActionApproval,
  });
  const pendingConnectorApprovals = approvalDialog.pendingApprovals;
  const targetSelection = useConsoleTargetSelection({
    messages,
    pendingApprovals: pendingConnectorApprovals,
    selectedTargetRef,
    setSearchParams,
    targets,
  });
  const { selectedRuntimeID, selectedTarget, targetItems, unreadMessages } = targetSelection;
  const selectedConnectorTemplate = selectedTarget ? getConnectorTemplate(selectedTarget.connector_kind) : null;
  const selectedTargetUsesLiveConsole = targetUsesLiveConsole(selectedTarget);
  const SelectedConnectorConsoleTemplate = selectedConnectorTemplate?.Console || null;
  const SelectedConnectorToolbarActions = selectedConnectorTemplate?.ToolbarActions || null;
  const ConnectorOperationTemplate = connectorOperation?.connector_kind
    ? getConnectorTemplate(connectorOperation.connector_kind)?.Operations || null
    : null;
  const {
    selectedRuntimeTarget,
    selectedSession: runtimeSelectedSession,
    selectedUnreadMessages,
  } = useConsolePageState({
    liveConsoleTargets,
    messages,
    sessions,
    selectedRuntimeID,
    allowTargetFallback: false,
  });
  const selectedTargetProfiles = targetSelection.selectedProfiles;
  const selectedTokenOptions = useMemo(() => {
    if (!selectedTarget) return [];
    return tokens.data.filter((token) => {
      if (token.revoked_at) return false;
      const profileID = selectedConnectorProfileID(token.id, selectedTarget, selectedTargetProfiles);
      return effectiveConnectorTargetProfilePermissions(connectorPermissionState.data[token.id] || [], selectedTarget, profileID, now).some(
        (permission) => permission.project_enabled !== false,
      );
    });
  }, [tokens.data, connectorPermissionState.data, selectedTarget, selectedTargetProfiles, now]);
  const selectedPendingConnectorApprovals = approvalDialog.selectedPendingApprovals;
  const alwaysRunTokenPermissions = useMemo(() => {
    if (!selectedTarget) return [];
    return selectedTokenOptions
      .map((token) => {
        const profileID = selectedConnectorProfileID(token.id, selectedTarget, selectedTargetProfiles);
        const permission = currentConnectorTargetProfilePermissions(
          connectorPermissionState.data[token.id] || [],
          selectedTarget,
          profileID,
        ).find((item) => effectiveRule(item, now) === "always_run");
        return permission ? { token, permission } : null;
      })
      .filter(Boolean);
  }, [selectedTokenOptions, connectorPermissionState.data, selectedTarget, selectedTargetProfiles, now]);
  const temporaryAlwaysRunLabels = alwaysRunTokenPermissions
    .map((item) => item.permission)
    .filter((permission) => permission?.expires_at)
    .map((permission) => permissionLifetimeLabel(permission, now));
  const showAlwaysRunWarning = Boolean(mcpRuntime?.data?.enabled && selectedTarget && alwaysRunTokenPermissions.length > 0);
  const selectedRecoverableRunningActions = recoverableRunningActions(selectedTarget);
  const selectedRunningConnectorRequests =
    selectedTarget && selectedRecoverableRunningActions.length > 0
      ? connectorActionApprovals.data.filter(
          (approval) =>
            approval.status === "running" &&
            approval.target_ref === selectedTarget.ref &&
            selectedRecoverableRunningActions.includes(approval.action_name),
        )
      : [];
  const selectedRunningRequest = selectedRunningConnectorRequests[0] || null;
  const workspaceSession = useConsoleWorkspaceSession({
    attachConsoleSession,
    newConsoleSession,
    onOpenConnectorOperation: openConnectorOperation,
    restartConsoleSession,
    runtimeSelectedSession,
    selectedRunningRequestID: selectedRunningRequest?.id,
    selectedRuntimeTarget,
    selectedTarget,
    selectedTargetUsesLiveConsole,
    sessions,
  });
  const { selectedSession, selectedSessionLive, selectedStructuredSession } = workspaceSession;
  const consoleBannerCount = (showAlwaysRunWarning ? 1 : 0) + (selectedRunningRequest ? 1 : 0) + (workspaceSession.newSessionError ? 1 : 0);
  const messageDialog = useConsoleMessages({
    loadMessages,
    markRuntimeMessagesRead,
    selectedRuntimeTarget,
    selectedSession,
    selectedSessionLive,
    selectedTokenOptions,
    selectedUnreadMessages,
  });

  useEffect(() => {
    if (tokens.state !== "ready") return;
    loadAllConnectorPermissions(tokens.data);
  }, [tokens.state, tokens.data, loadAllConnectorPermissions]);

  useEffect(() => {
    if (!selectedTarget?.ref) return;
    loadConnectorActions(selectedTarget);
  }, [selectedTarget, loadConnectorActions]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, []);

  function openConnectorOperation(operation) {
    if (!operation?.open || !operation?.connector_kind) return false;
    setConnectorOperation(operation);
    return true;
  }

  async function completeConnectorOperation(result, operation) {
    if (result?.startConsoleSession && operation?.runtimeTarget) {
      await workspaceSession.startNew(operation.runtimeTarget);
    }
  }

  return (
    <section className={`grid h-[calc(100vh-40px)] min-h-[640px] gap-4 ${consoleShellGridClass(targetsCompact, tokensCompact)}`}>
      <ConsoleTargetSidebar
        compact={targetsCompact}
        onCompactChange={setTargetsCompact}
        targetRows={targetSelection.targetRows}
        search={targetSelection.search}
        onSearch={targetSelection.setSearch}
        groups={targetSelection.groups}
        collapsedProjects={targetSelection.collapsedProjects}
        onToggleProject={targetSelection.toggleProject}
        targetItems={targetItems}
        liveConsoleTargets={liveConsoleTargets}
        sessions={sessions}
        selectedTarget={selectedTarget}
        pendingConnectorApprovals={pendingConnectorApprovals}
        connectorActionApprovals={connectorActionApprovals}
        unreadMessages={unreadMessages}
        onSelect={targetSelection.selectTarget}
        targetsState={targets.state}
        targetsError={targets.error}
        filteredTargetCount={targetSelection.filteredTargets.length}
      />

      <ConsoleWorkspacePanel
        theme={theme}
        approvals={connectorActionApprovals}
        liveConsoleTargets={liveConsoleTargets.data}
        connectorView={{ Console: SelectedConnectorConsoleTemplate, ToolbarActions: SelectedConnectorToolbarActions }}
        targetView={{
          runningApprovalCount: connectorActionApprovals.data.filter(
            (approval) => approval.status === "running" && selectedTarget && approval.target_ref === selectedTarget.ref,
          ).length,
          selectedPendingApprovals: selectedPendingConnectorApprovals,
          selectedRuntimeTarget,
          selectedTarget,
          selectedTargetProfiles,
          selectedUnreadMessages,
        }}
        sessionView={{
          selectedSession,
          selectedSessionLive,
          selectedStructuredSession,
          sessionsState: consoleSessions.state,
          targetUsesLiveConsole: selectedTargetUsesLiveConsole,
        }}
        warnings={{
          alwaysRunTokenCount: alwaysRunTokenPermissions.length,
          bannerCount: consoleBannerCount,
          newSessionError: workspaceSession.newSessionError,
          now,
          restartAction: workspaceSession.restartAction,
          runningRequest: selectedRunningRequest,
          showAlwaysRun: showAlwaysRunWarning,
          temporaryAlwaysRunLabels,
        }}
        actions={{
          endLiveSession: () => selectedSession.id && void closeConsoleSession(selectedSession.id),
          endStructuredSession: workspaceSession.endStructured,
          interruptSession: () => selectedSession.id && cancelConsoleCommand(selectedSession.id),
          openActivity: () => setConnectorActivityOpen(true),
          openApproval: approvalDialog.open,
          openMessages: () => messageDialog.open(),
          refreshActivity: loadConnectorActionApprovals,
          refreshSessions: loadConsoleSessions,
          resizeSession: (cols, rows) => selectedSession.id && resizeConsoleSession(selectedSession.id, cols, rows),
          restartSession: workspaceSession.restart,
          selectLiveSessionName: workspaceSession.selectLiveSessionName,
          selectProfile: targetSelection.selectProfile,
          sendInput: (data) => selectedSession.id && sendConsoleInput(selectedSession.id, data),
          startLiveSession: () => selectedRuntimeTarget && void workspaceSession.startNew(selectedRuntimeTarget),
          startLiveSessionWithOptions: (options = {}) => selectedRuntimeTarget && workspaceSession.startNew(selectedRuntimeTarget, options),
          startStructuredSession: workspaceSession.startStructured,
        }}
      />

      <TokenPermissionPanel
        tokens={tokens}
        selectedTarget={selectedTarget}
        targets={targets}
        unreadMessages={unreadMessages}
        compact={tokensCompact}
        connectorPermissionState={connectorPermissionState}
        loadAllConnectorPermissions={loadAllConnectorPermissions}
        loadConnectorActions={loadConnectorActions}
        replaceTokenConnectorPermissions={replaceTokenConnectorPermissions}
        onToggleCompact={() => setTokensCompact((current) => !current)}
        onOpenMessages={(tokenID) => messageDialog.open(tokenID)}
        onRefresh={async () => {
          const tokenItems = await loadTokens();
          await Promise.all([
            loadTargets(),
            loadAllConnectorPermissions(tokenItems),
            selectedTarget?.ref ? loadConnectorActions(selectedTarget) : Promise.resolve(),
          ]);
        }}
      />

      <ConnectorActionApprovalDialog
        approval={approvalDialog.activeApproval}
        note={approvalDialog.note}
        action={approvalDialog.action}
        onNoteChange={approvalDialog.setNote}
        onRun={approvalDialog.approve}
        onDecline={approvalDialog.decline}
        onClose={approvalDialog.close}
      />
      <ConnectorActivityDialog
        open={connectorActivityOpen}
        approvals={connectorActionApprovals}
        onRefresh={loadConnectorActionApprovals}
        onClose={() => setConnectorActivityOpen(false)}
      />
      <MessagesDialog
        open={messageDialog.isOpen}
        target={selectedRuntimeTarget}
        tokens={selectedTokenOptions}
        tokenID={messageDialog.tokenID}
        state={messageDialog.state}
        text={messageDialog.text}
        onTokenChange={messageDialog.setTokenID}
        onTextChange={messageDialog.setText}
        onSubmit={messageDialog.submit}
        onRefresh={messageDialog.load}
        onClose={messageDialog.close}
      />
      {ConnectorOperationTemplate ? (
        <ConnectorOperationTemplate
          value={connectorOperation}
          credentials={[]}
          onChange={setConnectorOperation}
          onOperationComplete={completeConnectorOperation}
        />
      ) : null}
    </section>
  );
}

function consoleShellGridClass(targetsCompact, tokensCompact) {
  if (targetsCompact && tokensCompact) return "grid-cols-[56px_minmax(0,1fr)_56px]";
  if (targetsCompact) return "grid-cols-[56px_minmax(0,1fr)_360px]";
  if (tokensCompact) return "grid-cols-[360px_minmax(0,1fr)_56px]";
  return "grid-cols-[360px_minmax(0,1fr)_360px]";
}
