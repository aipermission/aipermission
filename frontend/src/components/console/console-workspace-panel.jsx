import { AlertTriangle, TerminalSquare } from "lucide-react";
import { ConnectorTemplateNotFound } from "../../connectors/templates/registry";
import { Button } from "../ui/button";
import { Select } from "../ui/form";
import { Notice } from "../ui/notice";
import { ConsoleRecoveryPanel } from "./console-recovery-panel";
import { ConsoleStatusDot, selectedTargetStatus, targetDisplayName, targetProfileLabel, targetSubtitle } from "./console-target-sidebar";
import { NoLiveSession } from "./no-live-session";
import { PtyConsole } from "./pty-console";

export function ConsoleWorkspacePanel({ actions, approvals, connectorView, liveConsoleTargets, sessionView, targetView, theme, warnings }) {
  const ConsoleTemplate = connectorView.Console;
  const ToolbarActions = connectorView.ToolbarActions;
  const { selectedRuntimeTarget, selectedTarget, selectedTargetProfiles, selectedPendingApprovals } = targetView;
  const { selectedSession, selectedSessionLive, selectedStructuredSession, sessionsState, targetUsesLiveConsole } = sessionView;

  return (
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
              pendingCount: selectedPendingApprovals.length,
              runningCount: targetView.runningApprovalCount,
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
                onChange={(event) => actions.selectProfile(event.target.value)}
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
          {selectedPendingApprovals.length > 0 ? (
            <Button
              type="button"
              variant="ghost"
              className="h-9 border border-amber-500/70 bg-amber-950/30 px-3 text-amber-100 hover:bg-amber-900/40"
              onClick={() => actions.openApproval(selectedPendingApprovals[0])}
              title="Pending connector approvals for this target"
            >
              <AlertTriangle className="h-3.5 w-3.5" />
              {selectedPendingApprovals.length}
            </Button>
          ) : null}
          {ToolbarActions ? (
            <ToolbarActions
              theme={theme}
              selectedTarget={selectedTarget}
              selectedRuntimeTarget={selectedRuntimeTarget}
              selectedSession={selectedSession}
              selectedSessionLive={selectedSessionLive}
              selectedUnreadMessages={targetView.selectedUnreadMessages}
              liveConsoleTargets={liveConsoleTargets}
              onOpenMessages={actions.openMessages}
              onRefreshSessions={actions.refreshSessions}
              onNewSession={actions.startLiveSession}
              onEndSession={actions.endLiveSession}
              onInterrupt={actions.interruptSession}
              structuredSession={selectedStructuredSession}
              onNewStructuredSession={actions.startStructuredSession}
              onEndStructuredSession={actions.endStructuredSession}
            />
          ) : null}
        </div>
      </header>

      <div className={`grid h-full min-h-0 overflow-hidden ${consoleContentGridClass(warnings.bannerCount)}`}>
        {warnings.showAlwaysRun ? (
          <div className="sticky top-0 z-10 border-b border-red-800/50 bg-red-950 px-4 py-2 text-xs font-semibold text-red-50">
            MCP is started and {warnings.alwaysRunTokenCount} token{warnings.alwaysRunTokenCount === 1 ? "" : "s"} can run connector actions
            on this target without approval. Prefer prompt mode unless direct execution is intentional.
            {warnings.temporaryAlwaysRunLabels.length > 0 ? ` Temporary grant: ${warnings.temporaryAlwaysRunLabels[0]}.` : ""}
          </div>
        ) : null}
        {warnings.runningRequest ? (
          <ConsoleRecoveryPanel
            request={warnings.runningRequest}
            now={warnings.now}
            theme={theme}
            action={warnings.restartAction}
            onRestart={actions.restartSession}
          />
        ) : null}
        {warnings.newSessionError ? (
          <div className={`border-b px-4 py-2 ${theme === "light" ? "border-red-200 bg-red-50" : "border-red-900/60 bg-red-950/40"}`}>
            <Notice tone="bad">{warnings.newSessionError}</Notice>
          </div>
        ) : null}
        {selectedTarget && ConsoleTemplate ? (
          <ConsoleTemplate
            target={selectedTarget}
            approvals={approvals}
            theme={theme}
            session={targetUsesLiveConsole ? selectedSession : selectedStructuredSession}
            selectedSessionLive={selectedSessionLive}
            selectedRuntimeTarget={selectedRuntimeTarget}
            onNewStructuredSession={actions.startStructuredSession}
            onNewLiveSession={actions.startLiveSessionWithOptions}
            onSelectLiveSessionName={actions.selectLiveSessionName}
            onEndLiveSession={actions.endLiveSession}
            onOpenActivity={actions.openActivity}
            onRefreshActivity={actions.refreshActivity}
          >
            {targetUsesLiveConsole && selectedRuntimeTarget && selectedSessionLive ? (
              <PtyConsole
                key={selectedSession.id || selectedRuntimeTarget.id}
                target={selectedRuntimeTarget}
                session={selectedSession}
                onInput={actions.sendInput}
                onResize={actions.resizeSession}
                theme={theme}
              />
            ) : targetUsesLiveConsole && selectedRuntimeTarget && sessionsState === "loading" ? (
              <PanelMessage theme={theme}>Loading console sessions...</PanelMessage>
            ) : targetUsesLiveConsole && selectedRuntimeTarget ? (
              <NoLiveSession
                target={selectedRuntimeTarget}
                lastSession={selectedSession.id ? selectedSession : null}
                onNewSession={actions.startLiveSession}
                theme={theme}
              />
            ) : targetUsesLiveConsole ? (
              <PanelMessage theme={theme}>Select a live-console connector.</PanelMessage>
            ) : null}
          </ConsoleTemplate>
        ) : selectedTarget ? (
          <PanelMessage theme={theme} centered={false}>
            <ConnectorTemplateNotFound kind={selectedTarget.connector_kind} slot="console" />
          </PanelMessage>
        ) : (
          <PanelMessage theme={theme} centered={false}>
            Select a target.
          </PanelMessage>
        )}
      </div>
    </section>
  );
}

function PanelMessage({ children, centered = true, theme }) {
  return (
    <div
      className={`${centered ? "grid h-full place-items-center " : ""}p-4 text-sm ${theme === "light" ? "text-stone-500" : "text-stone-300"}`}
    >
      {children}
    </div>
  );
}

function consoleContentGridClass(bannerCount) {
  if (bannerCount >= 3) return "grid-rows-[auto_auto_auto_minmax(0,1fr)]";
  if (bannerCount === 2) return "grid-rows-[auto_auto_minmax(0,1fr)]";
  if (bannerCount === 1) return "grid-rows-[auto_minmax(0,1fr)]";
  return "grid-rows-[minmax(0,1fr)]";
}
