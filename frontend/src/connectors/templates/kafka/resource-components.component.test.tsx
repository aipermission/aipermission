import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { KafkaResourceBrowser } from "./resource-browser";
import { KafkaResourceDetail } from "./resource-detail";
import { KafkaOffsetDialog, KafkaPublishDialog } from "./write-dialogs";

const styles = connectorConsoleTheme("dark");

it("renders Kafka resource tabs, summaries, filtering, refresh, and selection", () => {
  const browser: ComponentProps<typeof KafkaResourceBrowser>["browser"] = {
    product: "Redpanda",
    view: "topics",
    query: "",
    filteredItems: [{ name: "orders", partition_count: 2, replication_factor: 3 }],
    selectedName: "orders",
    state: { state: "idle", error: "", message: "" },
    changeView: vi.fn(),
    setQuery: vi.fn(),
    refreshList: vi.fn(),
    selectItem: vi.fn(),
  };
  const { rerender } = render(<KafkaResourceBrowser browser={browser} styles={styles} />);
  expect(screen.getByRole("button", { name: /orders/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByText(/2 partitions/)).toHaveTextContent("replication 3");
  fireEvent.change(screen.getByLabelText("Filter topics"), { target: { value: "ord" } });
  expect(browser.setQuery).toHaveBeenCalledWith("ord");
  fireEvent.click(screen.getByRole("button", { name: "Refresh topics" }));
  fireEvent.click(screen.getByRole("button", { name: /orders/ }));
  expect(browser.selectItem).toHaveBeenCalledWith(browser.filteredItems[0]);
  fireEvent.click(screen.getByRole("tab", { name: "Groups" }));
  expect(browser.changeView).toHaveBeenCalledWith("groups");
  rerender(
    <KafkaResourceBrowser
      browser={{
        ...browser,
        view: "groups",
        filteredItems: [{ name: "reader", state: "Empty", protocol_type: "consumer" }],
        selectedName: "",
      }}
      styles={styles}
    />,
  );
  expect(screen.getByText("Empty · consumer")).toBeInTheDocument();
  rerender(
    <KafkaResourceBrowser
      browser={{ ...browser, filteredItems: [], state: { state: "loading", error: "", message: "" } }}
      styles={styles}
    />,
  );
  expect(screen.getByText("Loading topics...")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Refresh topics" })).toBeDisabled();
});

it("renders exact Kafka offsets and untrusted message data without executing HTML", () => {
  const browser: ComponentProps<typeof KafkaResourceDetail>["browser"] = {
    view: "topics",
    selectedName: "orders",
    activeDetail: { partitions: [{ partition: 1, end_offset: "9223372036854775807" }], extension: "retained" },
    messages: { messages: [{ value: "<script>throw Error('unsafe')</script>" }] },
    readForm: { partition: "1", start_position: "recent", offset: "9007199254740993", max_records: "20" },
    setReadForm: vi.fn(),
    state: { state: "idle", error: "", message: "" },
    readMessages: vi.fn(),
  };
  const writes = {
    openPublishDialog: vi.fn(),
    openOffsetDialog: vi.fn(),
    offsetPartitions: [{ topic: "orders", partition: 1, committed_offset: "9007199254740993", end_offset: "9223372036854775807" }],
  };
  const { container, rerender } = render(<KafkaResourceDetail browser={browser} writes={writes} styles={styles} />);
  expect(screen.getByText(/9223372036854775807/, { selector: "pre" })).toBeInTheDocument();
  expect(container.querySelector("script")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Read" }));
  expect(browser.readMessages).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "Publish" }));
  expect(writes.openPublishDialog).toHaveBeenCalledOnce();
  fireEvent.change(screen.getByLabelText("Start position"), { target: { value: "offset" } });
  expect(browser.setReadForm).toHaveBeenCalledWith(expect.any(Function));
  rerender(
    <KafkaResourceDetail
      browser={{
        ...browser,
        view: "groups",
        activeDetail: { members: [], partitions: writes.offsetPartitions },
        state: { state: "error", error: "denied", message: "" },
      }}
      writes={writes}
      styles={styles}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Set offset" }));
  expect(writes.openOffsetDialog).toHaveBeenCalledOnce();
  expect(screen.getByText("denied")).toBeInTheDocument();
});

it("updates Kafka publish fields and disables submission while pending", () => {
  const props: ComponentProps<typeof KafkaPublishDialog> = {
    value: {
      open: true,
      error: "",
      form: { partition: "0", key: "", key_encoding: "utf8", value: "hello", value_encoding: "utf8", headers: "[]" },
    },
    product: "Kafka",
    topic: "orders",
    partitions: [{ partition: 0 }, { partition: 1 }],
    pending: false,
    actionError: "",
    onChange: vi.fn(),
    onClose: vi.fn(),
    onConfirm: vi.fn(),
  };
  const { rerender } = render(<KafkaPublishDialog {...props} />);
  fireEvent.change(screen.getByLabelText("Partition"), { target: { value: "1" } });
  expect(props.onChange).toHaveBeenCalledWith({ ...props.value.form, partition: "1" });
  fireEvent.change(screen.getByLabelText("Value"), { target: { value: "changed" } });
  expect(props.onChange).toHaveBeenCalledWith({ ...props.value.form, value: "changed" });
  fireEvent.click(screen.getByRole("button", { name: "Publish message" }));
  expect(props.onConfirm).toHaveBeenCalledOnce();
  rerender(<KafkaPublishDialog {...props} pending actionError="publish denied" />);
  expect(screen.getByRole("button", { name: "Publishing..." })).toBeDisabled();
  expect(screen.getByRole("alert")).toHaveTextContent("publish denied");
  expect(screen.getByRole("button", { name: "Close dialog" })).toBeDisabled();
});

it("keeps exact offset strings visible and clears uncommitted partition defaults", () => {
  const props: ComponentProps<typeof KafkaOffsetDialog> = {
    value: { open: true, error: "", form: { selection: '["orders",0]', offset: "9007199254740993" } },
    product: "Kafka",
    group: "reader",
    partitions: [
      { topic: "orders", partition: 0, committed_offset: "9007199254740993", end_offset: "9223372036854775807", earliest_offset: "0" },
      { topic: "orders", partition: 1, committed_offset: "-1", end_offset: "0" },
    ],
    pending: false,
    actionError: "",
    onChange: vi.fn(),
    onClose: vi.fn(),
    onConfirm: vi.fn(),
  };
  render(<KafkaOffsetDialog {...props} />);
  expect(screen.getByLabelText("Current offset")).toHaveValue("9007199254740993");
  expect(screen.getByText(/Allowed range:/)).toHaveTextContent("0 to 9223372036854775807");
  fireEvent.change(screen.getByLabelText("Topic partition"), { target: { value: '["orders",1]' } });
  expect(props.onChange).toHaveBeenCalledWith({ selection: '["orders",1]', offset: "" });
  fireEvent.click(screen.getByRole("button", { name: "Change offset" }));
  expect(props.onConfirm).toHaveBeenCalledOnce();
});
