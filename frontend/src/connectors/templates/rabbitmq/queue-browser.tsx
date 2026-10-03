import { RefreshCcw, Search } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { connectorActionBusy } from "../_shared/action-state";
import { queueNameLabel, queueTotals } from "./helpers";
import type { RabbitBrowser } from "./use-rabbitmq-browser";
import type { RabbitQueue, RabbitStyles } from "./browser-types";

export function QueueBrowser({ browser, styles }: { browser: RabbitBrowser; styles: RabbitStyles }) {
  return (
    <section
      className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${styles.border} ${styles.subtlePanel}`}
    >
      <div className={`border-b p-3 ${styles.border}`}>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <p className="text-sm font-semibold">Queues</p>
            <p className={`text-xs ${styles.muted}`}>
              {browser.filteredQueues.length} shown · {browser.queues.length} loaded
            </p>
            <p className={`min-h-4 text-xs ${styles.muted}`}>
              {browser.queueDiscovery.partial ? "Partial queue list" : "Queue list"}
              {browser.queueDiscovery.scanLimitReached ? " · scan limit reached" : ""}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {browser.latestAction ? <Badge tone={actionTone(browser.latestAction.status)}>{browser.latestAction.action_name}</Badge> : null}
            <Button
              type="button"
              variant="outline"
              className="h-8 w-8 px-0"
              title="Refresh queues"
              aria-label="Refresh queues"
              onClick={browser.refreshQueues}
              disabled={connectorActionBusy(browser.state) || browser.publishLocked}
            >
              <RefreshCcw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </div>
      <div className={`grid gap-2 border-b p-3 ${styles.border}`}>
        <form
          className="grid grid-cols-[minmax(0,1fr)_auto] gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            void browser.refreshQueues();
          }}
        >
          <Input
            className={styles.input}
            value={browser.pattern}
            onChange={(event) => browser.setPattern(event.target.value)}
            placeholder="Filter queues"
            disabled={browser.publishLocked}
          />
          <Button
            type="submit"
            variant="outline"
            className="h-10 w-10 px-0"
            title="Search queues"
            aria-label="Search queues"
            disabled={connectorActionBusy(browser.state) || browser.publishLocked}
          >
            <Search className="h-4 w-4" />
          </Button>
        </form>
        <form
          className="grid grid-cols-[minmax(0,1fr)_auto] gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            browser.applyVhost();
          }}
        >
          <Input
            className={styles.input}
            value={browser.vhostDraft}
            onChange={(event) => browser.setVhostDraft(event.target.value)}
            placeholder="vhost"
            disabled={browser.publishLocked}
          />
          <Button type="submit" variant="outline" className="h-9" disabled={browser.publishLocked}>
            {browser.state.state === "loading" ? "Loading" : "Refresh"}
          </Button>
        </form>
      </div>
      <div className="min-h-0 overflow-auto p-2">
        {browser.filteredQueues.map((queue) => (
          <button
            key={`${queue.vhost || browser.vhost}:${queue.name}`}
            type="button"
            aria-label={queueNameLabel(queue.name)}
            aria-pressed={browser.activeQueue === queue.name}
            className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left text-sm transition ${browser.activeQueue === queue.name ? styles.activeRow : `${styles.border} ${styles.rowHover}`}`}
            onClick={() => browser.selectQueue(queue.name)}
            disabled={browser.publishLocked}
          >
            <span className="truncate font-mono text-xs font-semibold" title={queue.name}>
              {queueNameLabel(queue.name)}
            </span>
            <span className={`text-xs ${browser.activeQueue === queue.name ? "" : styles.muted}`}>
              ready {numberText(queue.messages_ready)} · unacked {numberText(queue.messages_unacknowledged)} · consumers{" "}
              {numberText(queue.consumers)}
            </span>
          </button>
        ))}
        {browser.filteredQueues.length === 0 ? <Notice>{emptyQueueMessage(browser)}</Notice> : null}
      </div>
      <QueueTotalsStrip queues={browser.queues} mutedClass={styles.muted} borderClass={styles.border} />
    </section>
  );
}

function emptyQueueMessage(browser: RabbitBrowser) {
  if (browser.state.state === "loading") return "Loading RabbitMQ queues...";
  if (browser.pattern.trim() !== browser.queueDiscovery.appliedPattern) return "No loaded queues match.";
  return browser.queueDiscovery.partial ? "No queues shown in this partial result." : "No queues found for this vhost/filter.";
}

function QueueTotalsStrip({
  queues,
  mutedClass,
  borderClass,
}: {
  queues: readonly RabbitQueue[];
  mutedClass: string;
  borderClass: string;
}) {
  const totals = queueTotals(queues);
  return (
    <div className={`grid gap-1 border-t p-3 text-xs ${borderClass}`}>
      <span>Loaded queue totals</span>
      <span className={mutedClass}>
        queues {queues.length} · messages {totals.messages} · ready {totals.ready} · unacked {totals.unacked} · consumers {totals.consumers}
      </span>
    </div>
  );
}

function actionTone(status: string | undefined) {
  if (status === "failed") return "bad";
  if (status === "completed") return "good";
  return "warn";
}

function numberText(value: unknown) {
  return value === undefined || value === null || value === "" ? "0" : String(value);
}
