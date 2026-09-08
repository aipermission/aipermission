import { RotateCcw, TerminalSquare } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { resourceSubtitle, resourceTitle } from "./helpers";
import { KubernetesPodConsolePanel } from "./pod-console-panel";
import { KubernetesHeaderStatus, KubernetesResourceDetail } from "./resource-detail";

export function KubernetesResourceWorkspace({
  browser,
  restart,
  target,
  theme,
  session,
  selectedRuntimeTarget,
  onEndLiveSession,
  children,
  styles,
}) {
  const resource = browser.selectedResource;
  return (
    <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${styles.border}`}>
      <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${styles.border} ${styles.subtlePanel}`}>
        <div className="min-w-0">
          <p className="text-sm font-semibold">{resource ? resourceTitle(browser.tab, resource) : browser.activeTab.label}</p>
          <p className={`truncate text-xs ${styles.muted}`}>
            {resource ? resourceSubtitle(browser.tab, resource) : "Select a resource to inspect details, logs, events, or raw JSON."}
          </p>
          <KubernetesHeaderStatus state={browser.state} mutedClass={styles.muted} />
        </div>
        <ResourceActions browser={browser} restart={restart} />
      </div>
      <div className="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] overflow-hidden p-3">
        {browser.tab === "pods" && browser.viewMode === "console" ? (
          <KubernetesPodConsolePanel
            target={target}
            pod={resource}
            selectedRuntimeTarget={selectedRuntimeTarget}
            session={session}
            sessionLive={browser.selectedPodConsoleLive}
            pending={browser.consolePending}
            theme={theme}
            mutedClass={styles.muted}
            borderClass={styles.border}
            onStart={() => browser.startPodConsole(resource)}
            onEnd={onEndLiveSession}
          >
            {children}
          </KubernetesPodConsolePanel>
        ) : (
          <KubernetesResourceDetail
            tab={browser.tab}
            resource={resource}
            detail={browser.detail}
            logs={browser.logs}
            search={browser.resultSearch}
            onSearch={browser.setResultSearch}
            inputClass={styles.input}
            mutedClass={styles.muted}
          />
        )}
      </div>
    </section>
  );
}

function ResourceActions({ browser, restart }) {
  const resource = browser.selectedResource;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {resource && browser.tab === "pods" ? (
        <>
          <Button
            type="button"
            variant="outline"
            className="h-8 px-2 text-xs"
            onClick={() => browser.readLogs(resource)}
            disabled={browser.state.state !== "idle"}
          >
            Logs
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-8 w-8 px-0"
            onClick={() => browser.openPodConsole(resource)}
            disabled={browser.state.state !== "idle"}
            title="Open live console inside this pod"
            aria-label="Open live console inside this pod"
          >
            <TerminalSquare className="h-3.5 w-3.5" />
          </Button>
        </>
      ) : null}
      {resource && browser.tab === "workloads" && resource.kind === "Deployment" ? (
        <Button
          type="button"
          variant="outline"
          className="h-8 px-2 text-xs"
          onClick={() => restart.open(resource)}
          disabled={browser.state.state !== "idle"}
        >
          <RotateCcw className="h-3.5 w-3.5" />
          Restart
        </Button>
      ) : null}
    </div>
  );
}
