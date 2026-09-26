import { useEffect, useRef, useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { actionableOffsetPartitions, offsetSelectionValue, parseOffsetSelection } from "./console-helpers";
import type { Dispatch, SetStateAction } from "react";
import type { useKafkaBrowser } from "./use-kafka-browser";
import type { KafkaOffsetForm, KafkaPublishForm, KafkaWriteDialog } from "./console-types";

const defaultPublish = Object.freeze({ partition: "0", key: "", key_encoding: "utf8", value: "", value_encoding: "utf8", headers: "[]" });
const defaultOffset = Object.freeze({ selection: "", offset: "" });

export function useKafkaWrites({ browser }: { browser: ReturnType<typeof useKafkaBrowser> }) {
  const [publishDialog, setPublishDialog] = useState<KafkaWriteDialog<KafkaPublishForm>>({ open: false, form: defaultPublish, error: "" });
  const [offsetDialog, setOffsetDialog] = useState<KafkaWriteDialog<KafkaOffsetForm>>({ open: false, form: defaultOffset, error: "" });
  const offsetPartitions = actionableOffsetPartitions(browser.activeDetail?.partitions);
  const writeScope = `${browser.scopeKey}:${browser.view}:${browser.selectedName}`;
  const requestGuard = useRequestGuard(writeScope);
  const submitting = useRef(false);
  const [pendingWrite, setPendingWrite] = useState("");

  useEffect(() => {
    submitting.current = false;
    setPendingWrite("");
    setPublishDialog({ open: false, form: defaultPublish, error: "" });
    setOffsetDialog({ open: false, form: defaultOffset, error: "" });
  }, [writeScope]);

  function invalidateWrite() {
    requestGuard.invalidate("write");
    submitting.current = false;
    setPendingWrite("");
  }

  async function runWrite(channel: string, operation: () => Promise<unknown>, onSuccess: () => Promise<unknown>) {
    if (submitting.current) return;
    const request = requestGuard.begin("write");
    submitting.current = true;
    setPendingWrite(channel);
    try {
      const output = await operation();
      if (request.isCurrent() && output) await onSuccess();
    } finally {
      if (request.isCurrent()) {
        submitting.current = false;
        setPendingWrite("");
      }
      request.complete();
    }
  }

  function openPublishDialog() {
    invalidateWrite();
    const firstPartition = browser.activeDetail?.partitions?.[0]?.partition ?? 0;
    browser.setState({ state: "idle", error: "", message: "" });
    setPublishDialog({ open: true, form: { ...defaultPublish, partition: String(firstPartition) }, error: "" });
  }

  async function publishMessage() {
    if (submitting.current || !publishDialog.open) return;
    const form = publishDialog.form;
    const headers = parseHeaders(form.headers);
    if (headers.error) {
      setPublishDialog((current) => ({ ...current, error: headers.error }));
      return;
    }
    setPublishDialog((current) => ({ ...current, error: "" }));
    await runWrite(
      "publish",
      () =>
        browser.runAction(
          "publish_message",
          {
            topic: browser.selectedName,
            partition: Number(form.partition),
            key: form.key,
            key_encoding: form.key_encoding,
            value: form.value,
            value_encoding: form.value_encoding,
            headers: headers.value,
          },
          `manual ${browser.product} browser message publish`,
          "writing",
          "publish",
        ),
      async () => {
        setPublishDialog({ open: false, form: defaultPublish, error: "" });
        await browser.loadDetail(browser.selectedName, "topics");
      },
    );
  }

  function openOffsetDialog() {
    invalidateWrite();
    const first = offsetPartitions[0];
    const selection = first ? offsetSelectionValue(first) : "";
    const offset = first?.committed_offset === "-1" ? "" : String(first?.committed_offset || "");
    browser.setState({ state: "idle", error: "", message: "" });
    setOffsetDialog({ open: true, form: { selection, offset }, error: "" });
  }

  async function setConsumerGroupOffset() {
    if (submitting.current || !offsetDialog.open) return;
    const selected = parseOffsetSelection(offsetDialog.form.selection);
    if (!selected) {
      setOffsetDialog((current) => ({ ...current, error: "Choose one topic partition." }));
      return;
    }
    if (!/^\d+$/.test(offsetDialog.form.offset.trim())) {
      setOffsetDialog((current) => ({ ...current, error: "New offset must be a non-negative integer." }));
      return;
    }
    setOffsetDialog((current) => ({ ...current, error: "" }));
    await runWrite(
      "offset",
      () =>
        browser.runAction(
          "set_consumer_group_offset",
          { group: browser.selectedName, topic: selected.topic, partition: selected.partition, offset: offsetDialog.form.offset.trim() },
          `manual ${browser.product} browser consumer group offset change`,
          "writing",
          "offset",
        ),
      async () => {
        setOffsetDialog({ open: false, form: defaultOffset, error: "" });
        await browser.loadDetail(browser.selectedName, "groups");
      },
    );
  }

  function updateDialog<Form>(setDialog: Dispatch<SetStateAction<KafkaWriteDialog<Form>>>, form: Form) {
    if (browser.state.state !== "writing" && browser.state.error) browser.setState((current) => ({ ...current, error: "" }));
    setDialog((current) => ({ ...current, form, error: "" }));
  }

  return {
    publishDialog,
    offsetDialog,
    publishPending: pendingWrite === "publish",
    offsetPending: pendingWrite === "offset",
    offsetPartitions,
    openPublishDialog,
    publishMessage,
    openOffsetDialog,
    setConsumerGroupOffset,
    updatePublishForm: (form: KafkaPublishForm) => updateDialog(setPublishDialog, form),
    updateOffsetForm: (form: KafkaOffsetForm) => updateDialog(setOffsetDialog, form),
    closePublish: () => {
      invalidateWrite();
      setPublishDialog({ open: false, form: defaultPublish, error: "" });
    },
    closeOffset: () => {
      invalidateWrite();
      setOffsetDialog({ open: false, form: defaultOffset, error: "" });
    },
  };
}

export function parseHeaders(value: string): { value: unknown[] | null; error: string } {
  try {
    const headers: unknown = JSON.parse(value || "[]");
    return Array.isArray(headers) ? { value: headers, error: "" } : { value: null, error: "Headers must be a JSON array." };
  } catch {
    return { value: null, error: "Headers must be valid JSON." };
  }
}
