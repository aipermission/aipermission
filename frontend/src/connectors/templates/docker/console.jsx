import { connectorConsoleTheme } from "../_shared/console-theme";
import { DockerLifecycleDialog } from "./lifecycle-dialog";
import { DockerResourceBrowser } from "./resource-browser";
import { DockerResourcePane } from "./resource-pane";
import { useDockerBrowser } from "./use-docker-browser";

export function DockerConnectorConsoleTemplate({
  children,
  target,
  approvals,
  theme,
  session,
  selectedSessionLive,
  selectedRuntimeTarget,
  onNewLiveSession,
  onSelectLiveSessionName,
  onEndLiveSession,
  onRefreshActivity,
}) {
  const classes = dockerConsoleClasses(theme);
  const browser = useDockerBrowser({
    target,
    approvals,
    session,
    selectedSessionLive,
    onNewLiveSession,
    onSelectLiveSessionName,
    onRefreshActivity,
  });

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${classes.panel}`}>
      <div className="grid h-full min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[360px_minmax(0,1fr)]">
        <DockerResourceBrowser
          resourceView={browser.resourceView}
          items={browser.filteredItems}
          visibleCount={browser.visibleCount}
          selectedContainer={browser.selectedContainer}
          selectedResourceID={browser.selectedResourceID}
          filter={browser.filter}
          state={browser.state}
          latestAction={browser.latestAction}
          theme={theme}
          classes={classes}
          onRefresh={() => void browser.refreshResource(browser.resourceView)}
          onSwitchView={browser.switchResourceView}
          onFilter={browser.setFilter}
          onSelect={(item) => browser.selectResource(browser.resourceView, item)}
        />

        <DockerResourcePane
          resourceView={browser.resourceView}
          selectedResource={browser.selectedResource}
          selectedContainer={browser.selectedContainer}
          containerRef={browser.selectedContainerRef}
          viewMode={browser.viewMode}
          result={browser.result}
          resultSearch={browser.resultSearch}
          tail={browser.tail}
          state={browser.state}
          target={target}
          selectedRuntimeTarget={selectedRuntimeTarget}
          session={session}
          sessionLive={browser.selectedContainerConsoleLive}
          consolePending={browser.consolePending}
          theme={theme}
          classes={classes}
          onTailChange={browser.setTail}
          onResultSearch={browser.setResultSearch}
          onReadLogs={() => void browser.readLogs()}
          onInspect={() => void browser.inspectContainer()}
          onOpenConsole={() => browser.openContainerConsole()}
          onStartConsole={browser.startContainerConsole}
          onEndConsole={onEndLiveSession}
          onLifecycle={browser.openLifecycle}
        >
          {children}
        </DockerResourcePane>
      </div>
      <DockerEndpointFooter target={target} borderClass={classes.border} mutedClass={classes.muted} />
      <DockerLifecycleDialog dialog={browser.confirmDialog} onClose={browser.closeConfirmDialog} onConfirm={browser.confirmLifecycle} />
    </div>
  );
}

function dockerConsoleClasses(theme) {
  const classes = connectorConsoleTheme(theme);
  return {
    panel: classes.panel,
    muted: classes.muted,
    border: classes.border,
    subtlePanel: classes.subtlePanel,
    input: classes.input,
    rowHover: classes.rowHover,
    activeRow: classes.activeRow,
  };
}

function DockerEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-h-[44px] items-center justify-between gap-3 border-t px-4 py-2 text-xs ${borderClass}`}>
      <span className={mutedClass}>Docker transport</span>
      <span className="truncate font-mono">{target.config?.transport_target_ref || "not configured"}</span>
    </div>
  );
}
