import { Gauge, Send } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { offsetSelectionValue } from "./console-helpers";

export function KafkaPublishDialog({ value, theme, product, topic, partitions, pending, actionError, onChange, onClose, onConfirm }) {
  const dark = theme !== "light";
  const inputClass = dark ? "border-stone-700 bg-[#1a1a1a] text-stone-100" : "";
  return (
    <Dialog
      open={value.open}
      title={`Publish ${product} message`}
      description="Review this bounded write before publishing."
      onClose={onClose}
      size="lg"
      closeDisabled={pending}
      closeOnOverlay={!pending}
      closeOnEscape={!pending}
      className={`grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] ${dark ? "border-stone-700 bg-[#252526] text-stone-100" : ""}`}
      bodyClassName={`min-h-0 overflow-y-auto ${dark ? "bg-[#252526]" : ""}`}
    >
      <div className="grid gap-4">
        <Notice tone="warn">
          This writes one message to {topic}. AIPermission requests all-in-sync-replica acknowledgements and does not create topics
          automatically.
        </Notice>
        <div className="grid gap-3 sm:grid-cols-2">
          <FieldBlock label="Partition">
            <Select
              className={inputClass}
              value={value.form.partition}
              disabled={pending}
              onChange={(event) => onChange({ ...value.form, partition: event.target.value })}
            >
              {partitions.map((partition) => (
                <option key={partition.partition} value={partition.partition}>
                  Partition {partition.partition}
                </option>
              ))}
            </Select>
          </FieldBlock>
          <FieldBlock label="Key encoding">
            <Select
              className={inputClass}
              value={value.form.key_encoding}
              disabled={pending}
              onChange={(event) => onChange({ ...value.form, key_encoding: event.target.value })}
            >
              <option value="utf8">UTF-8</option>
              <option value="base64">Base64</option>
            </Select>
          </FieldBlock>
        </div>
        <FieldBlock label="Key">
          <Input
            className={`font-mono ${inputClass}`}
            value={value.form.key}
            disabled={pending}
            onChange={(event) => onChange({ ...value.form, key: event.target.value })}
            placeholder="Optional message key"
          />
        </FieldBlock>
        <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_140px]">
          <FieldBlock label="Value">
            <Textarea
              className={`min-h-40 resize-y font-mono text-xs ${inputClass}`}
              value={value.form.value}
              disabled={pending}
              onChange={(event) => onChange({ ...value.form, value: event.target.value })}
              placeholder='{"type":"test"}'
            />
          </FieldBlock>
          <FieldBlock label="Value encoding">
            <Select
              className={inputClass}
              value={value.form.value_encoding}
              disabled={pending}
              onChange={(event) => onChange({ ...value.form, value_encoding: event.target.value })}
            >
              <option value="utf8">UTF-8</option>
              <option value="base64">Base64</option>
            </Select>
          </FieldBlock>
        </div>
        <FieldBlock label="Headers JSON">
          <Textarea
            id="kafka-publish-headers"
            className={`min-h-24 resize-y font-mono text-xs ${inputClass}`}
            value={value.form.headers}
            disabled={pending}
            aria-invalid={Boolean(value.error || actionError)}
            aria-describedby={value.error || actionError ? "kafka-publish-error" : undefined}
            onChange={(event) => onChange({ ...value.form, headers: event.target.value })}
            placeholder='[{"key":"trace-id","value":"abc","encoding":"utf8"}]'
          />
        </FieldBlock>
        {value.error || actionError ? (
          <div id="kafka-publish-error" role="alert" aria-live="assertive">
            <Notice tone="bad">{value.error || actionError}</Notice>
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button type="button" onClick={onConfirm} disabled={pending || !topic || partitions.length === 0}>
            <Send className="h-4 w-4" />
            {pending ? "Publishing..." : "Publish message"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

export function KafkaOffsetDialog({ value, theme, product, group, partitions, pending, actionError, onChange, onClose, onConfirm }) {
  const dark = theme !== "light";
  const inputClass = dark ? "border-stone-700 bg-[#1a1a1a] text-stone-100" : "";
  const selected = partitions.find((partition) => offsetSelectionValue(partition) === value.form.selection);
  return (
    <Dialog
      open={value.open}
      title={`Set ${product} consumer group offset`}
      description="This can replay or skip messages for one partition."
      onClose={onClose}
      size="md"
      closeDisabled={pending}
      closeOnOverlay={!pending}
      closeOnEscape={!pending}
      className={`grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] ${dark ? "border-stone-700 bg-[#252526] text-stone-100" : ""}`}
      bodyClassName={`min-h-0 overflow-y-auto ${dark ? "bg-[#252526]" : ""}`}
    >
      <div className="grid gap-4">
        <Notice tone="bad">
          AIPermission checks that the group is inactive immediately before committing and verifies the stored offset afterward. Kafka does
          not provide an atomic lock against a member joining during that interval. For MCP access, keep this destructive action on Prompt.
        </Notice>
        <FieldBlock label="Topic partition">
          <Select
            className={inputClass}
            value={value.form.selection}
            disabled={pending}
            onChange={(event) => {
              const next = partitions.find((partition) => offsetSelectionValue(partition) === event.target.value);
              onChange({
                selection: event.target.value,
                offset: next?.committed_offset === "-1" ? "" : String(next?.committed_offset || ""),
              });
            }}
          >
            {partitions.map((partition) => (
              <option key={offsetSelectionValue(partition)} value={offsetSelectionValue(partition)}>
                {partition.topic} / p{partition.partition} · committed {partition.committed_offset} · end {partition.end_offset}
              </option>
            ))}
          </Select>
        </FieldBlock>
        <div className="grid gap-3 sm:grid-cols-2">
          <FieldBlock label="Current offset">
            <Input className={inputClass} value={String(selected?.committed_offset ?? "not committed")} disabled />
          </FieldBlock>
          <FieldBlock label="New offset">
            <Input
              id="kafka-offset-value"
              className={`font-mono ${inputClass}`}
              inputMode="numeric"
              value={value.form.offset}
              disabled={pending}
              aria-invalid={Boolean(value.error || actionError)}
              aria-describedby={value.error || actionError ? "kafka-offset-error" : undefined}
              onChange={(event) => onChange({ ...value.form, offset: event.target.value })}
              placeholder="Exact offset"
            />
          </FieldBlock>
        </div>
        <p className="text-xs text-stone-500">
          Allowed range:{" "}
          {selected?.committed_offset == null
            ? "select a partition"
            : `${selected.earliest_offset ?? "unknown"} to ${selected.end_offset ?? "unknown"}`}{" "}
          · group {group}
        </p>
        {value.error || actionError ? (
          <div id="kafka-offset-error" role="alert" aria-live="assertive">
            <Notice tone="bad">{value.error || actionError}</Notice>
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="danger"
            onClick={onConfirm}
            disabled={pending || !group || !value.form.selection || !value.form.offset.trim()}
          >
            <Gauge className="h-4 w-4" />
            {pending ? "Changing..." : "Change offset"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function FieldBlock({ label, children }) {
  return (
    <label className="grid gap-1.5 text-xs font-semibold">
      <span className="uppercase text-stone-500">{label}</span>
      {children}
    </label>
  );
}
