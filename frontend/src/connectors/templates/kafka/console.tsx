import { Activity, Database } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { ConnectorEndpointFooter } from "../_shared/endpoint-footer";
import { StructuredSessionEmpty } from "../_shared/structured-session-empty";
import { KafkaResourceBrowser } from "./resource-browser";
import { KafkaResourceDetail } from "./resource-detail";
import { useKafkaBrowser } from "./use-kafka-browser";
import { useKafkaWrites } from "./use-kafka-writes";
import { KafkaOffsetDialog, KafkaPublishDialog } from "./write-dialogs";
import type { KafkaBrowserProps } from "./console-types";
import type { ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";

export function KafkaConnectorConsoleTemplate({
  target,
  approvals,
  theme = "dark",
  session,
  onNewStructuredSession,
  onRefreshActivity,
}: KafkaBrowserProps & { theme?: string; onNewStructuredSession?: () => void }) {
  const browser = useKafkaBrowser({ target, approvals, session, onRefreshActivity });
  const writes = useKafkaWrites({ browser });
  const styles = connectorConsoleTheme(theme);

  if (!browser.activeSession.active) {
    return (
      <StructuredSessionEmpty
        icon={Database}
        title={`No active ${browser.product} session`}
        description="Start a structured session to browse topics, consumer groups, lag, and bounded message samples."
        buttonLabel="New session"
        onStart={() => onNewStructuredSession?.()}
        panelClass={styles.panel}
        mutedClass={styles.muted}
        compact
      />
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${styles.panel}`}>
      <div className="grid min-h-0 gap-4 overflow-y-auto p-4 xl:grid-cols-[360px_minmax(0,1fr)] xl:overflow-hidden">
        <KafkaResourceBrowser browser={browser} styles={styles} />
        <KafkaResourceDetail browser={browser} writes={writes} styles={styles} />
      </div>
      <ConnectorEndpointFooter
        leading={target.ref}
        borderClass={styles.border}
        mutedClass={styles.muted}
        className="border-t px-3 py-2"
        trailing={
          <>
            <LatestAction value={browser.latestAction} />
            <span>{brokerList(target)}</span>
          </>
        }
      />
      <KafkaPublishDialog
        value={writes.publishDialog}
        theme={theme}
        product={browser.product}
        topic={browser.selectedName}
        partitions={browser.activeDetail?.partitions || []}
        pending={writes.publishPending || browser.state.state === "writing"}
        actionError={browser.state.error}
        onChange={writes.updatePublishForm}
        onClose={writes.closePublish}
        onConfirm={() => void writes.publishMessage()}
      />
      <KafkaOffsetDialog
        value={writes.offsetDialog}
        theme={theme}
        product={browser.product}
        group={browser.selectedName}
        partitions={writes.offsetPartitions}
        pending={writes.offsetPending || browser.state.state === "writing"}
        actionError={browser.state.error}
        onChange={writes.updateOffsetForm}
        onClose={writes.closeOffset}
        onConfirm={() => void writes.setConsumerGroupOffset()}
      />
    </div>
  );
}

function LatestAction({ value }: { value: Pick<ConnectorApproval, "status" | "action_name"> | null }) {
  if (!value) return <Activity className="h-3.5 w-3.5" />;
  return <Badge tone={value.status === "completed" ? "good" : value.status === "failed" ? "bad" : "warn"}>{value.action_name}</Badge>;
}

function brokerList(target: KafkaBrowserProps["target"]) {
  return String(target.config?.bootstrap_brokers || "")
    .split(/[\s,]+/)
    .filter(Boolean)
    .join(", ");
}
