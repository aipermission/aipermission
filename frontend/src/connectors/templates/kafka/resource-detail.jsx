import { Eye, Gauge, Send } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";

export function KafkaResourceDetail({ browser, writes, styles }) {
  return (
    <section className={`grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${styles.border}`}>
      <DetailHeader browser={browser} writes={writes} styles={styles} />
      <div className="grid min-h-0 gap-4 overflow-y-auto p-4 lg:grid-cols-2 lg:overflow-hidden">
        <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2">
          <p className={`text-xs font-semibold uppercase ${styles.muted}`}>{browser.view === "topics" ? "Topic metadata" : "Group and lag"}</p>
          <TerminalBlock surface="log" className="min-h-0 text-xs">{browser.activeDetail ? JSON.stringify(browser.activeDetail, null, 2) : "No item selected."}</TerminalBlock>
        </div>
        <DetailOutput browser={browser} styles={styles} />
      </div>
      <div className={`grid gap-2 border-t p-3 ${styles.border}`} aria-live="polite">
        <Notice tone="warn">Message values, keys, and headers can contain secrets. Samples are bounded and never commit consumer offsets.</Notice>
        <Notice tone="warn">Publishing writes one message. Offset changes can replay or skip messages. Local browser writes require confirmation here; for MCP access, keep both actions on Prompt unless direct execution is intentional.</Notice>
        {browser.state.error ? <Notice tone="bad">{browser.state.error}</Notice> : null}
        {browser.state.message ? <Notice tone="good">{browser.state.message}</Notice> : null}
      </div>
    </section>
  );
}

function DetailHeader({ browser, writes, styles }) {
  return (
    <div className={`flex items-center justify-between gap-3 border-b p-3 ${styles.border} ${styles.subtlePanel}`}>
      <div className="min-w-0">
        <p className="truncate text-sm font-semibold">{browser.selectedName || (browser.view === "topics" ? "Topic detail" : "Consumer group detail")}</p>
        <p className={`truncate text-xs ${styles.muted}`}>{browser.selectedName ? browser.view === "topics" ? "Partitions, offsets, and bounded message samples" : "Members, assignments, committed offsets, and lag" : `Select one of the ${browser.view} on the left.`}</p>
      </div>
      <div className="flex items-center gap-2">
        {browser.activeDetail && browser.view === "topics" ? <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={writes.openPublishDialog} disabled={browser.state.state !== "idle"}><Send className="h-3.5 w-3.5" />Publish</Button> : null}
        {browser.activeDetail && browser.view === "groups" && writes.offsetPartitions.length > 0 ? <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={writes.openOffsetDialog} disabled={browser.state.state !== "idle"}><Gauge className="h-3.5 w-3.5" />Set offset</Button> : null}
        {browser.activeDetail ? <CopyButton value={JSON.stringify({ detail: browser.activeDetail, messages: browser.messages }, null, 2)} variant="outline" className="h-8 px-2 text-xs">JSON</CopyButton> : null}
      </div>
    </div>
  );
}

function DetailOutput({ browser, styles }) {
  return (
    <div className="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-2">
      <p className={`text-xs font-semibold uppercase ${styles.muted}`}>{browser.view === "topics" ? "Message sample" : "Assignments"}</p>
      {browser.view === "topics" ? <ReadControls browser={browser} styles={styles} /> : <span />}
      <TerminalBlock surface="log" className="min-h-0 text-xs">{outputText(browser)}</TerminalBlock>
    </div>
  );
}

function ReadControls({ browser, styles }) {
  const update = (values) => browser.setReadForm((current) => ({ ...current, ...values }));
  const numberField = browser.readForm.start_position === "offset" ? "offset" : "max_records";
  return (
    <div className="grid gap-2 sm:grid-cols-[100px_140px_110px_minmax(0,1fr)_auto]">
      <Select className={styles.input} value={browser.readForm.partition} onChange={(event) => update({ partition: event.target.value })} aria-label="Partition">{(browser.activeDetail?.partitions || []).map((partition) => <option value={partition.partition} key={partition.partition}>p{partition.partition}</option>)}</Select>
      <Select className={styles.input} value={browser.readForm.start_position} onChange={(event) => update({ start_position: event.target.value })} aria-label="Start position"><option value="recent">Recent</option><option value="earliest">Earliest</option><option value="offset">Offset</option></Select>
      <Input className={styles.input} type="number" min="0" value={browser.readForm[numberField]} onChange={(event) => update({ [numberField]: event.target.value })} aria-label={numberField === "offset" ? "Offset" : "Maximum records"} />
      <span />
      <Button type="button" className="h-9" disabled={!browser.selectedName || browser.state.state !== "idle"} onClick={() => void browser.readMessages()}><Eye className="h-4 w-4" />Read</Button>
    </div>
  );
}

function outputText(browser) {
  if (browser.view === "topics") return browser.messages ? JSON.stringify(browser.messages, null, 2) : "No messages read in this session.";
  return browser.activeDetail ? JSON.stringify({ members: browser.activeDetail.members || [], partitions: browser.activeDetail.partitions || [] }, null, 2) : "No consumer group selected.";
}
