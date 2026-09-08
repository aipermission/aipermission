import { Database } from "lucide-react";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { ConnectorEndpointFooter } from "../_shared/endpoint-footer";
import { StructuredSessionEmpty } from "../_shared/structured-session-empty";
import { QueueBrowser } from "./queue-browser";
import { QueueDetail } from "./queue-detail";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";

export function RabbitMQConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const browser = useRabbitMQBrowser({ target, approvals, session, onRefreshActivity });
  const styles = connectorConsoleTheme(theme);

  if (!browser.activeSession.active) {
    return (
      <StructuredSessionEmpty
        icon={Database}
        title="No active RabbitMQ session"
        description="Start a structured session to browse queues through the connector approval, history, and audit pipeline."
        buttonLabel="Start RabbitMQ session"
        onStart={onNewStructuredSession}
        panelClass={styles.panel}
        mutedClass={styles.muted}
        footer={<RabbitEndpointFooter target={target} borderClass={styles.border} mutedClass={styles.muted} />}
      />
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${styles.panel}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 xl:grid-cols-[360px_minmax(0,1fr)]">
        <QueueBrowser browser={browser} styles={styles} />
        <QueueDetail browser={browser} styles={styles} />
      </div>
      <RabbitEndpointFooter target={target} borderClass={styles.border} mutedClass={styles.muted} />
    </div>
  );
}

function RabbitEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <ConnectorEndpointFooter
      leading={target.ref}
      trailing={`${target.config?.scheme || "http"}://${target.config?.host}:${target.config?.port || 15672} · vhost ${target.config?.vhost || "/"}`}
      borderClass={borderClass}
      mutedClass={mutedClass}
      className="border-t px-3 py-2"
    />
  );
}
