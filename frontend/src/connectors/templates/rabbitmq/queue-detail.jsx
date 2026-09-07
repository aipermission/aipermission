import { Eye, ListTree, Send } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { formatMessages, queueMetaText } from "./helpers";
import { RabbitPublishForm } from "./publish-form";

export function QueueDetail({ browser, styles }) {
  const publishing = browser.detailMode === "publish";
  return (
    <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${styles.border}`}>
      <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${styles.border} ${styles.subtlePanel}`}>
        <div className="min-w-0">
          <p className="text-sm font-semibold">{publishing ? "Publish message" : browser.activeQueue || "Queue detail"}</p>
          <p className={`truncate text-xs ${styles.muted}`}>{detailHelp(browser)}</p>
        </div>
        <div className="flex items-center gap-2">
          {!publishing && browser.queueDetail?.state ? (
            <Badge tone={browser.queueDetail.state === "running" ? "good" : "warn"}>{browser.queueDetail.state}</Badge>
          ) : null}
          {!publishing && browser.queueDetail ? (
            <CopyButton
              value={JSON.stringify({ queue: browser.queueDetail, bindings: browser.bindings, messages: browser.messages }, null, 2)}
              variant="outline"
              className="h-8 px-2 text-xs"
              title="Copy queue JSON"
            >
              JSON
            </CopyButton>
          ) : null}
        </div>
      </div>
      <DetailToolbar browser={browser} styles={styles} />
      <div className="min-h-0 overflow-hidden p-4">
        {publishing ? <RabbitPublishForm browser={browser} styles={styles} /> : <QueueInspection browser={browser} styles={styles} />}
      </div>
      <div className={`grid gap-3 border-t p-3 ${styles.border}`}>
        <Notice tone="warn">
          Peek uses ack_requeue_true with bounded count and payload truncation. Avoid reading payloads unless the operator approved that
          access.
        </Notice>
        <Notice tone="warn">Publish creates a new RabbitMQ message and uses the write permission for this connector action.</Notice>
        {browser.state.error ? <Notice tone="bad">{browser.state.error}</Notice> : null}
        {browser.state.message ? <Notice tone="good">{browser.state.message}</Notice> : null}
      </div>
    </section>
  );
}

function DetailToolbar({ browser, styles }) {
  const publishing = browser.detailMode === "publish";
  return (
    <div className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${styles.border}`}>
      <div className="flex min-w-0 items-center gap-2">
        {publishing ? (
          <>
            <Send className={`h-4 w-4 ${styles.muted}`} />
            <span className={`text-xs ${styles.muted}`}>New write action</span>
          </>
        ) : (
          <>
            <ListTree className={`h-4 w-4 ${styles.muted}`} />
            <span className={`text-xs ${styles.muted}`}>{browser.bindings.length} binding(s)</span>
          </>
        )}
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">
        {publishing ? (
          <Button type="button" variant="outline" className="h-8 px-3 text-xs" onClick={() => browser.setDetailMode("inspect")}>
            Back to detail
          </Button>
        ) : (
          <>
            <Input
              className={`h-8 w-24 ${styles.input}`}
              type="number"
              min="1"
              max="50"
              value={browser.peekCount}
              onChange={(event) => browser.setPeekCount(event.target.value)}
              aria-label="Peek count"
            />
            <Button
              type="button"
              className="h-8 px-3 text-xs"
              disabled={!browser.activeQueue || browser.state.state !== "idle"}
              onClick={browser.peekMessages}
            >
              <Eye className="h-3.5 w-3.5" />
              Peek
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-8 px-3 text-xs"
              disabled={browser.state.state !== "idle"}
              onClick={browser.startPublish}
            >
              <Send className="h-3.5 w-3.5" />
              Publish
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

function QueueInspection({ browser, styles }) {
  return (
    <div className="grid h-full min-h-0 gap-4 overflow-hidden lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
        <p className={`text-xs font-semibold uppercase ${styles.muted}`}>Queue and bindings</p>
        <TerminalBlock surface="log" className="min-h-0 text-xs">
          {browser.queueDetail ? JSON.stringify({ queue: browser.queueDetail, bindings: browser.bindings }, null, 2) : "No queue selected."}
        </TerminalBlock>
      </div>
      <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
        <p className={`text-xs font-semibold uppercase ${styles.muted}`}>Messages</p>
        <TerminalBlock surface="log" className="min-h-0 text-xs">
          {browser.messages.length ? formatMessages(browser.messages) : "No messages peeked in this session."}
        </TerminalBlock>
      </div>
    </div>
  );
}

function detailHelp(browser) {
  if (browser.detailMode === "publish")
    return browser.activeQueue
      ? `Publishing to ${browser.activeQueue} through amq.default uses routing_key=${browser.activeQueue}.`
      : "Create one RabbitMQ message through the connector write permission.";
  return browser.queueDetail
    ? queueMetaText(browser.queueDetail)
    : "Select a queue to inspect counters, bindings, and bounded message previews.";
}
