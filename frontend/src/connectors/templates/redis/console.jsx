import { Database } from "lucide-react";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { StructuredSessionEmpty } from "../_shared/structured-session-empty";
import { RedisConfirmDialog } from "./confirm-dialog";
import { RedisEndpointFooter } from "./endpoint-footer";
import { RedisKeyBrowser } from "./key-browser";
import { useRedisBrowser } from "./use-redis-browser";
import { RedisValueWorkspace } from "./value-workspace";

export function RedisConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const browser = useRedisBrowser({ target, approvals, session, onRefreshActivity });
  const styles = connectorConsoleTheme(theme);
  const footer = <RedisEndpointFooter target={target} borderClass={styles.border} mutedClass={styles.muted} />;

  if (!browser.activeSession.active) {
    return (
      <StructuredSessionEmpty
        icon={Database}
        title={`No active ${browser.product} session`}
        description={`Start a structured session to browse ${browser.product} keys through the connector approval, history, and audit pipeline.`}
        buttonLabel={`Start ${browser.product} session`}
        onStart={onNewStructuredSession}
        panelClass={styles.panel}
        mutedClass={styles.muted}
        footer={footer}
      />
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${styles.panel}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[340px_minmax(0,1fr)]">
        <RedisKeyBrowser browser={browser} styles={styles} />
        <RedisValueWorkspace browser={browser} styles={styles} />
      </div>
      {footer}
      <RedisConfirmDialog
        value={browser.confirmDialog}
        theme={theme}
        product={browser.product}
        onClose={browser.closeConfirmDialog}
        onConfirm={browser.confirmPendingAction}
      />
    </div>
  );
}
