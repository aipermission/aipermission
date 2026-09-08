import { RefreshCcw, Search } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { queueTotals } from "./helpers";

export function QueueBrowser({ browser, styles }) {
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
              disabled={browser.state.state !== "idle"}
            >
              <RefreshCcw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </div>
      <form
        className={`grid gap-2 border-b p-3 ${styles.border}`}
        onSubmit={(event) => {
          event.preventDefault();
          void browser.refreshQueues();
        }}
      >
        <div className="relative">
          <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${styles.muted}`} />
          <Input
            className={`pl-9 ${styles.input}`}
            value={browser.pattern}
            onChange={(event) => browser.setPattern(event.target.value)}
            placeholder="Filter queues"
          />
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
          <Input
            className={styles.input}
            value={browser.vhost}
            onChange={(event) => browser.setVhost(event.target.value)}
            placeholder="vhost"
          />
          <Button type="submit" variant="outline" className="h-9" disabled={browser.state.state !== "idle"}>
            {browser.state.state === "loading" ? "Loading" : "Refresh"}
          </Button>
        </div>
      </form>
      <div className="min-h-0 overflow-auto p-2">
        {browser.filteredQueues.map((queue) => (
          <button
            key={`${queue.vhost || browser.vhost}:${queue.name}`}
            type="button"
            aria-pressed={browser.activeQueue === queue.name}
            className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left text-sm transition ${browser.activeQueue === queue.name ? styles.activeRow : `${styles.border} ${styles.rowHover}`}`}
            onClick={() => browser.selectQueue(queue.name)}
          >
            <span className="truncate font-mono text-xs font-semibold" title={queue.name}>
              {queue.name}
            </span>
            <span className={`text-xs ${browser.activeQueue === queue.name ? "" : styles.muted}`}>
              ready {numberText(queue.messages_ready)} · unacked {numberText(queue.messages_unacknowledged)} · consumers{" "}
              {numberText(queue.consumers)}
            </span>
          </button>
        ))}
        {browser.filteredQueues.length === 0 ? (
          <Notice>{browser.state.state === "loading" ? "Loading RabbitMQ queues..." : "No queues found for this vhost/filter."}</Notice>
        ) : null}
      </div>
      <QueueTotalsStrip queues={browser.queues} mutedClass={styles.muted} borderClass={styles.border} />
    </section>
  );
}

function QueueTotalsStrip({ queues, mutedClass, borderClass }) {
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

function actionTone(status) {
  if (status === "failed") return "bad";
  if (status === "completed") return "good";
  return "warn";
}

function numberText(value) {
  return value === undefined || value === null || value === "" ? "0" : String(value);
}
