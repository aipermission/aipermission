import { connectorConsoleTheme } from "./console-theme";
import { SQLConsoleWorkspace } from "./sql-console-workspace";
import { SQLEndpointFooter, SQLNoSessionPlaceholder } from "./sql-console-chrome";
import { SQLQueryForm } from "./sql-query-form";
import { useSQLConsole } from "./use-sql-console";

export { SQLConnectorToolbarActions } from "./sql-console-chrome";

export function SQLConnectorConsole(props) {
  const { target, theme, onNewStructuredSession } = props;
  const controller = useSQLConsole(props);
  const styles = connectorConsoleTheme(theme);
  const footer = <SQLEndpointFooter config={controller.connector} target={target} borderClass={styles.border} mutedClass={styles.muted} />;

  if (!controller.activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${styles.panel}`}>
        <SQLNoSessionPlaceholder config={controller.connector} target={target} theme={theme} onNewSession={onNewStructuredSession} />
        {footer}
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] ${styles.panel}`}>
      <SQLQueryForm controller={controller} styles={styles} theme={theme} />
      <SQLConsoleWorkspace controller={controller} styles={styles} theme={theme} />
      {footer}
    </div>
  );
}
