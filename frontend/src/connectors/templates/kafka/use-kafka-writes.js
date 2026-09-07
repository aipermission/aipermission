import { useEffect, useState } from "react";
import { actionableOffsetPartitions, offsetSelectionValue, parseOffsetSelection } from "./console-helpers";

const defaultPublish = Object.freeze({ partition: "0", key: "", key_encoding: "utf8", value: "", value_encoding: "utf8", headers: "[]" });
const defaultOffset = Object.freeze({ selection: "", offset: "" });

export function useKafkaWrites({ browser }) {
  const [publishDialog, setPublishDialog] = useState({ open: false, form: defaultPublish, error: "" });
  const [offsetDialog, setOffsetDialog] = useState({ open: false, form: defaultOffset, error: "" });
  const offsetPartitions = actionableOffsetPartitions(browser.activeDetail?.partitions);

  useEffect(() => {
    setPublishDialog({ open: false, form: defaultPublish, error: "" });
    setOffsetDialog({ open: false, form: defaultOffset, error: "" });
  }, [browser.scopeKey]);

  function openPublishDialog() {
    const firstPartition = browser.activeDetail?.partitions?.[0]?.partition ?? 0;
    browser.setState({ state: "idle", error: "", message: "" });
    setPublishDialog({ open: true, form: { ...defaultPublish, partition: String(firstPartition) }, error: "" });
  }

  async function publishMessage() {
    const form = publishDialog.form;
    const headers = parseHeaders(form.headers);
    if (headers.error) {
      setPublishDialog((current) => ({ ...current, error: headers.error }));
      return;
    }
    setPublishDialog((current) => ({ ...current, error: "" }));
    const output = await browser.runAction(
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
    );
    if (!output) return;
    setPublishDialog({ open: false, form: defaultPublish, error: "" });
    await browser.loadDetail(browser.selectedName, "topics");
  }

  function openOffsetDialog() {
    const first = offsetPartitions[0];
    const selection = first ? offsetSelectionValue(first) : "";
    const offset = first?.committed_offset === "-1" ? "" : String(first?.committed_offset || "");
    browser.setState({ state: "idle", error: "", message: "" });
    setOffsetDialog({ open: true, form: { selection, offset }, error: "" });
  }

  async function setConsumerGroupOffset() {
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
    const output = await browser.runAction(
      "set_consumer_group_offset",
      { group: browser.selectedName, topic: selected.topic, partition: selected.partition, offset: offsetDialog.form.offset.trim() },
      `manual ${browser.product} browser consumer group offset change`,
      "writing",
      "offset",
    );
    if (!output) return;
    setOffsetDialog({ open: false, form: defaultOffset, error: "" });
    await browser.loadDetail(browser.selectedName, "groups");
  }

  function updateDialog(setDialog, form) {
    if (browser.state.state !== "writing" && browser.state.error) browser.setState((current) => ({ ...current, error: "" }));
    setDialog((current) => ({ ...current, form, error: "" }));
  }

  return {
    publishDialog,
    offsetDialog,
    offsetPartitions,
    openPublishDialog,
    publishMessage,
    openOffsetDialog,
    setConsumerGroupOffset,
    updatePublishForm: (form) => updateDialog(setPublishDialog, form),
    updateOffsetForm: (form) => updateDialog(setOffsetDialog, form),
    closePublish: () => setPublishDialog({ open: false, form: defaultPublish, error: "" }),
    closeOffset: () => setOffsetDialog({ open: false, form: defaultOffset, error: "" }),
  };
}

export function parseHeaders(value) {
  try {
    const headers = JSON.parse(value || "[]");
    return Array.isArray(headers) ? { value: headers, error: "" } : { value: null, error: "Headers must be a JSON array." };
  } catch {
    return { value: null, error: "Headers must be valid JSON." };
  }
}
