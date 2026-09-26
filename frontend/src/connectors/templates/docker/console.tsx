import { connectorConsoleTheme } from "../_shared/console-theme";
import { DockerLifecycleDialog } from "./lifecycle-dialog";
import { DockerResourceBrowser } from "./resource-browser";
import { DockerResourcePane } from "./resource-pane";
import { useDockerBrowser } from "./use-docker-browser";
import type { DockerBrowserProps } from "./use-docker-browser";
import type { ComponentProps, ReactNode } from "react";

type DockerConsoleProps = DockerBrowserProps & Pick<ComponentProps<typeof DockerResourcePane>, "theme" | "selectedRuntimeTarget"> & {
  children?: ReactNode;
  onEndLiveSession?: () => unknown;
};

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
}: DockerConsoleProps) {
  const classes = connectorConsoleTheme(theme);
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

function DockerEndpointFooter({ target, borderClass, mutedClass }: { target: DockerBrowserProps["target"]; borderClass: string; mutedClass: string }) {
  return (
    <div className={`flex min-h-[44px] items-center justify-between gap-3 border-t px-4 py-2 text-xs ${borderClass}`}>
      <span className={mutedClass}>Docker transport</span>
      <span className="truncate font-mono">{target.config?.transport_target_ref || "not configured"}</span>
    </div>
  );
}
