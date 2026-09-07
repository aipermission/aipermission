import { AlertTriangle, TerminalSquare } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import {
  currentConnectorTargetProfilePermissions,
  effectiveConnectorTargetProfilePermissions,
  selectedConnectorProfileID,
} from "../lib/connector-permissions";
import { useGateway } from "../lib/gateway-context";
import { effectiveRule, permissionLifetimeLabel } from "../lib/permissions";
import { useConnectorPermissions } from "../lib/use-connector-permissions";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { ConnectorActionApprovalDialog } from "../components/console/connector-action-approval-dialog";
import { ConnectorActivityDialog } from "../components/console/connector-activity-dialog";
import { ConsoleRecoveryPanel } from "../components/console/console-recovery-panel";
import {
  ConsoleStatusDot,
  ConsoleTargetSidebar,
  recoverableRunningActions,
  selectedTargetStatus,
  targetDisplayName,
  targetProfileLabel,
  targetSubtitle,
  targetUsesLiveConsole,
} from "../components/console/console-target-sidebar";
import { MessagesDialog } from "../components/console/messages-dialog";
import { NoLiveSession } from "../components/console/no-live-session";
import { PtyConsole } from "../components/console/pty-console";
import { TokenPermissionPanel } from "../components/console/token-permission-panel";
import { isLiveConsoleSession } from "../components/console/helpers";
import { useConsolePageState } from "../components/console/use-console-page-state";
import { useConsoleMessages } from "../components/console/use-console-messages";
import { useConsoleTargetSelection } from "../components/console/use-console-target-selection";
import { useConnectorApprovalDialog } from "../components/console/use-connector-approval-dialog";
import { ConnectorTemplateNotFound, getConnectorModel, getConnectorTemplate } from "../connectors/templates/registry";

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
  const [restartAction, setRestartAction] = useState({ state: "idle", error: null });
  const [newSessionError, setNewSessionError] = useState("");
  const [now, setNow] = useState(Date.now());
  const [structuredSessionsByTarget, setStructuredSessionsByTarget] = useState({});
  const [liveSessionNameByTarget, setLiveSessionNameByTarget] = useState({});

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
  const selectedStructuredSession =
    selectedTarget && !selectedTargetUsesLiveConsole ? structuredSessionsByTarget[selectedTarget.ref] || null : null;
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
  const selectedLiveSessionName = selectedTarget?.ref ? liveSessionNameByTarget[selectedTarget.ref] || "" : "";
  const selectedNamedLiveSession =
    selectedRuntimeTarget && selectedLiveSessionName
      ? sessions.find(
          (session) => Number(session.runtime_id) === Number(selectedRuntimeTarget.id) && session.name === selectedLiveSessionName,
        )
      : null;
  const selectedSession = selectedNamedLiveSession || runtimeSelectedSession;
  const selectedSessionLive = isLiveConsoleSession(selectedSession);
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
  const consoleBannerCount = (showAlwaysRunWarning ? 1 : 0) + (selectedRunningRequest ? 1 : 0) + (newSessionError ? 1 : 0);
  const messageDialog = useConsoleMessages({
    loadMessages,
    markRuntimeMessagesRead,
    selectedRuntimeTarget,
    selectedSession,
    selectedSessionLive,
    selectedTokenOptions,
    selectedUnreadMessages,
  });

  const attachSelectedConsoleSession = useEffectEvent((sessionID) => attachConsoleSession(sessionID));

  useEffect(() => {
    if (tokens.state !== "ready") return;
    loadAllConnectorPermissions(tokens.data);
  }, [tokens.state, tokens.data, loadAllConnectorPermissions]);

  useEffect(() => {
    if (!selectedTarget?.ref) return;
    loadConnectorActions(selectedTarget);
  }, [selectedTarget, loadConnectorActions]);

  useEffect(() => {
    if (!selectedTarget || selectedTargetUsesLiveConsole) return;
    setStructuredSessionsByTarget((current) => {
      if (current[selectedTarget.ref]) return current;
      return { ...current, [selectedTarget.ref]: newStructuredConsoleSession() };
    });
  }, [selectedTarget, selectedTargetUsesLiveConsole]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!selectedRuntimeTarget) return;
    if (selectedSessionLive) {
      attachSelectedConsoleSession(selectedSession.id);
    }
  }, [selectedRuntimeTarget, selectedSessionLive, selectedSession.id]);

  useEffect(() => {
    setRestartAction({ state: "idle", error: null });
    setNewSessionError("");
  }, [selectedRuntimeTarget?.id, selectedRunningRequest?.id]);

  function openConnectorOperation(operation) {
    if (!operation?.open || !operation?.connector_kind) return false;
    setConnectorOperation(operation);
    return true;
  }

  async function completeConnectorOperation(result, operation) {
    if (result?.startConsoleSession && operation?.runtimeTarget) {
      await startNewConsoleSession(operation.runtimeTarget);
    }
  }

  async function startNewConsoleSession(runtimeTarget, options = {}) {
    if (!runtimeTarget) return;
    setNewSessionError("");
    if (options.name && selectedTarget?.ref) {
      setLiveSessionNameByTarget((current) => ({ ...current, [selectedTarget.ref]: options.name }));
    }
    try {
      await newConsoleSession(runtimeTarget, options);
    } catch (error) {
      const model = getConnectorModel(runtimeTarget.connector_kind);
      const operation = model?.operationFromError?.(error, { operation: "new-session", target: runtimeTarget });
      if (openConnectorOperation(operation)) return;
      setNewSessionError(error.message || "Console session could not be started.");
    }
  }

  async function restartSelectedConsoleSession() {
    if (!selectedRuntimeTarget) return;
    setRestartAction({ state: "running", error: null });
    try {
      await restartConsoleSession(selectedRuntimeTarget.id);
      setRestartAction({ state: "idle", error: null });
    } catch (error) {
      setRestartAction({ state: "error", error: error.message });
    }
  }

  function startStructuredConnectorSession() {
    if (!selectedTarget || selectedTargetUsesLiveConsole) return;
    setStructuredSessionsByTarget((current) => ({ ...current, [selectedTarget.ref]: newStructuredConsoleSession() }));
  }

  function selectLiveSessionName(name) {
    if (!selectedTarget?.ref || !name) return;
    setLiveSessionNameByTarget((current) => ({ ...current, [selectedTarget.ref]: name }));
  }

  function endStructuredConnectorSession() {
    if (!selectedTarget || selectedTargetUsesLiveConsole) return;
    setStructuredSessionsByTarget((current) => ({ ...current, [selectedTarget.ref]: { active: false, startedAt: "" } }));
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

      <section
        className={`grid h-full min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border shadow-xl ${
          theme === "light" ? "border-stone-200 bg-white" : "border-stone-800 bg-[#1e1e1e]"
        }`}
      >
        <header
          className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b px-4 py-3 ${
            theme === "light" ? "border-stone-200 bg-stone-50 text-stone-950" : "border-stone-700 bg-[#2d2d2d] text-stone-100"
          }`}
        >
          <div className="flex min-w-0 items-center gap-3">
            <ConsoleStatusDot
              status={selectedTargetStatus({
                target: selectedTarget,
                session: selectedSession,
                pendingCount: selectedPendingConnectorApprovals.length,
                runningCount: connectorActionApprovals.data.filter(
                  (approval) => approval.status === "running" && selectedTarget && approval.target_ref === selectedTarget.ref,
                ).length,
              })}
            />
            <div className="min-w-0">
              <h3 className="flex min-w-0 items-center gap-2 text-sm font-semibold">
                <TerminalSquare className="h-4 w-4 shrink-0" />
                <span className="truncate">{selectedTarget ? targetDisplayName(selectedTarget) : "Console"}</span>
              </h3>
              {selectedTarget ? (
                <p className={`truncate text-xs ${theme === "light" ? "text-stone-500" : "text-stone-400"}`}>
                  {targetSubtitle(selectedTarget, selectedRuntimeTarget)}
                </p>
              ) : null}
            </div>
            {selectedTargetProfiles.length > 1 ? (
              <label
                className={`ml-2 hidden min-w-36 max-w-48 shrink-0 items-center gap-2 text-xs font-semibold lg:flex ${theme === "light" ? "text-stone-600" : "text-stone-300"}`}
              >
                Profile
                <Select
                  className={`h-8 ${theme === "light" ? "" : "border-stone-700 bg-[#1e1e1e] text-stone-100"}`}
                  value={selectedTarget?.profile_id ? String(selectedTarget.profile_id) : ""}
                  onChange={(event) => targetSelection.selectProfile(event.target.value)}
                >
                  {selectedTargetProfiles.map((profile) => (
                    <option key={profile.profile_id} value={profile.profile_id}>
                      {targetProfileLabel(profile)}
                    </option>
                  ))}
                </Select>
              </label>
            ) : null}
          </div>
          <div className="flex shrink-0 gap-2">
            {selectedPendingConnectorApprovals.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                className="h-9 border border-amber-500/70 bg-amber-950/30 px-3 text-amber-100 hover:bg-amber-900/40"
                onClick={() => approvalDialog.open(selectedPendingConnectorApprovals[0])}
                title="Pending connector approvals for this target"
              >
                <AlertTriangle className="h-3.5 w-3.5" />
                {selectedPendingConnectorApprovals.length}
              </Button>
            ) : null}
            {SelectedConnectorToolbarActions ? (
              <SelectedConnectorToolbarActions
                theme={theme}
                selectedTarget={selectedTarget}
                selectedRuntimeTarget={selectedRuntimeTarget}
                selectedSession={selectedSession}
                selectedSessionLive={selectedSessionLive}
                selectedUnreadMessages={selectedUnreadMessages}
                liveConsoleTargets={liveConsoleTargets.data}
                onOpenMessages={() => messageDialog.open()}
                onRefreshSessions={loadConsoleSessions}
                onNewSession={() => selectedRuntimeTarget && void startNewConsoleSession(selectedRuntimeTarget)}
                onEndSession={() => selectedSession.id && void closeConsoleSession(selectedSession.id)}
                onInterrupt={() => selectedSession.id && cancelConsoleCommand(selectedSession.id)}
                structuredSession={selectedStructuredSession}
                onNewStructuredSession={startStructuredConnectorSession}
                onEndStructuredSession={endStructuredConnectorSession}
              />
            ) : null}
          </div>
        </header>

        <div className={`grid h-full min-h-0 overflow-hidden ${consoleContentGridClass(consoleBannerCount)}`}>
          {showAlwaysRunWarning ? (
            <div className="sticky top-0 z-10 border-b border-red-800/50 bg-red-950 px-4 py-2 text-xs font-semibold text-red-50">
              MCP is started and {alwaysRunTokenPermissions.length} token{alwaysRunTokenPermissions.length === 1 ? "" : "s"} can run
              connector actions on this target without approval. Prefer prompt mode unless direct execution is intentional.
              {temporaryAlwaysRunLabels.length > 0 ? ` Temporary grant: ${temporaryAlwaysRunLabels[0]}.` : ""}
            </div>
          ) : null}
          {selectedRunningRequest ? (
            <ConsoleRecoveryPanel
              request={selectedRunningRequest}
              now={now}
              theme={theme}
              action={restartAction}
              onRestart={restartSelectedConsoleSession}
            />
          ) : null}
          {newSessionError ? (
            <div className={`border-b px-4 py-2 ${theme === "light" ? "border-red-200 bg-red-50" : "border-red-900/60 bg-red-950/40"}`}>
              <Notice tone="bad">{newSessionError}</Notice>
            </div>
          ) : null}
          {selectedTarget && SelectedConnectorConsoleTemplate ? (
            <SelectedConnectorConsoleTemplate
              target={selectedTarget}
              approvals={connectorActionApprovals}
              theme={theme}
              session={selectedTargetUsesLiveConsole ? selectedSession : selectedStructuredSession}
              selectedSessionLive={selectedSessionLive}
              selectedRuntimeTarget={selectedRuntimeTarget}
              onNewStructuredSession={startStructuredConnectorSession}
              onNewLiveSession={(options = {}) => selectedRuntimeTarget && startNewConsoleSession(selectedRuntimeTarget, options)}
              onSelectLiveSessionName={selectLiveSessionName}
              onEndLiveSession={() => selectedSession.id && closeConsoleSession(selectedSession.id)}
              onOpenActivity={() => setConnectorActivityOpen(true)}
              onRefreshActivity={loadConnectorActionApprovals}
            >
              {selectedTargetUsesLiveConsole && selectedRuntimeTarget && selectedSessionLive ? (
                <PtyConsole
                  key={selectedSession.id || selectedRuntimeTarget.id}
                  target={selectedRuntimeTarget}
                  session={selectedSession}
                  onInput={(data) => selectedSession.id && sendConsoleInput(selectedSession.id, data)}
                  onResize={(cols, rows) => selectedSession.id && resizeConsoleSession(selectedSession.id, cols, rows)}
                  theme={theme}
                />
              ) : selectedTargetUsesLiveConsole && selectedRuntimeTarget && consoleSessions.state === "loading" ? (
                <div className={`grid h-full place-items-center p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}>
                  Loading console sessions...
                </div>
              ) : selectedTargetUsesLiveConsole && selectedRuntimeTarget ? (
                <NoLiveSession
                  target={selectedRuntimeTarget}
                  lastSession={selectedSession.id ? selectedSession : null}
                  onNewSession={() => void startNewConsoleSession(selectedRuntimeTarget)}
                  theme={theme}
                />
              ) : selectedTargetUsesLiveConsole ? (
                <div className={`p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}>
                  Select a live-console connector.
                </div>
              ) : null}
            </SelectedConnectorConsoleTemplate>
          ) : selectedTarget ? (
            <div className={`p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}>
              <ConnectorTemplateNotFound kind={selectedTarget.connector_kind} slot="console" />
            </div>
          ) : (
            <div className={`p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}>Select a target.</div>
          )}
        </div>
      </section>

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

function consoleContentGridClass(bannerCount) {
  if (bannerCount >= 3) return "grid-rows-[auto_auto_auto_minmax(0,1fr)]";
  if (bannerCount === 2) return "grid-rows-[auto_auto_minmax(0,1fr)]";
  if (bannerCount === 1) return "grid-rows-[auto_minmax(0,1fr)]";
  return "grid-rows-[minmax(0,1fr)]";
}

function newStructuredConsoleSession() {
  return { active: true, startedAt: new Date().toISOString() };
}
