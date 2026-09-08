import { connectorConsoleTheme } from "../_shared/console-theme";
import { KubernetesResourceBrowser } from "./resource-browser";
import { KubernetesFooter } from "./resource-detail";
import { KubernetesResourceWorkspace } from "./resource-workspace";
import { KubernetesRestartDialog } from "./restart-dialog";
import { useKubernetesBrowser } from "./use-kubernetes-browser";
import { useRolloutRestart } from "./use-rollout-restart";

export function KubernetesConnectorConsoleTemplate(props) {
  const { children, target, theme, session, selectedRuntimeTarget, onEndLiveSession } = props;
  const browser = useKubernetesBrowser(props);
  const restart = useRolloutRestart({
    targetRef: target.ref,
    tab: browser.tab,
    selectedResource: browser.selectedResource,
    runAction: browser.runAction,
    refreshResource: browser.refreshResource,
  });
  const styles = connectorConsoleTheme(theme);

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${styles.panel}`}>
      <div className="grid h-full min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[380px_minmax(0,1fr)]">
        <KubernetesResourceBrowser browser={browser} styles={styles} theme={theme} />
        <KubernetesResourceWorkspace
          browser={browser}
          restart={restart}
          target={target}
          theme={theme}
          session={session}
          selectedRuntimeTarget={selectedRuntimeTarget}
          onEndLiveSession={onEndLiveSession}
          styles={styles}
        >
          {children}
        </KubernetesResourceWorkspace>
      </div>
      <KubernetesFooter target={target} borderClass={styles.border} mutedClass={styles.muted} />
      <KubernetesRestartDialog restart={restart} styles={styles} />
    </div>
  );
}
