import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { useGateway } from "../lib/gateway-context";
import { useConnectorPermissions } from "../lib/use-connector-permissions";
import { ConsolePageDialogs } from "../components/console/console-page-dialogs";
import { consoleShellGridClass } from "../components/console/console-layout";
import { ConsoleTargetSidebar, targetUsesLiveConsole } from "../components/console/console-target-sidebar";
import { ConsoleWorkspacePanel } from "../components/console/console-workspace-panel";
import { TokenPermissionPanel } from "../components/console/token-permission-panel";
import { useConsolePageState } from "../components/console/use-console-page-state";
import { useConsoleMessages } from "../components/console/use-console-messages";
import { useConsoleConnectorView } from "../components/console/use-console-connector-view";
import { useConsolePermissionView } from "../components/console/use-console-permission-view";
import { useConsoleRecoveryState } from "../components/console/use-console-recovery-state";
import { useConsoleTargetSelection } from "../components/console/use-console-target-selection";
import { useConsoleWorkspaceSession } from "../components/console/use-console-workspace-session";
import { useConnectorApprovalDialog } from "../components/console/use-connector-approval-dialog";

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
  const connectorView = useConsoleConnectorView({ selectedTarget });
  const selectedTargetUsesLiveConsole = targetUsesLiveConsole(selectedTarget);
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
  const selectedPendingConnectorApprovals = approvalDialog.selectedPendingApprovals;
  const { now, runningRequest: selectedRunningRequest } = useConsoleRecoveryState({
    approvals: connectorActionApprovals.data,
    selectedTarget,
  });
  const { alwaysRunTokenPermissions, selectedTokenOptions, showAlwaysRunWarning, temporaryAlwaysRunLabels } = useConsolePermissionView({
    connectorPermissions: connectorPermissionState.data,
    mcpEnabled: mcpRuntime?.data?.enabled,
    now,
    profiles: selectedTargetProfiles,
    target: selectedTarget,
    tokens: tokens.data,
  });
  const workspaceSession = useConsoleWorkspaceSession({
    attachConsoleSession,
    newConsoleSession,
    onOpenConnectorOperation: connectorView.openOperation,
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
        connectorView={connectorView}
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
          openActivity: connectorView.openActivity,
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

      <ConsolePageDialogs
        activityDialog={{
          approvals: connectorActionApprovals,
          close: connectorView.closeActivity,
          open: connectorView.activityOpen,
          refresh: loadConnectorActionApprovals,
        }}
        approvalDialog={approvalDialog}
        messageDialog={{ ...messageDialog, target: selectedRuntimeTarget, tokens: selectedTokenOptions }}
        operationDialog={{
          onChange: connectorView.setOperation,
          onComplete: completeConnectorOperation,
          Template: connectorView.OperationTemplate,
          value: connectorView.operation,
        }}
      />
    </section>
  );
}
