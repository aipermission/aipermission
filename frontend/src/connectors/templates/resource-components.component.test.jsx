import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { KafkaResourceBrowser } from "./kafka/resource-browser";
import { KafkaResourceDetail } from "./kafka/resource-detail";
import { KubernetesResourceBrowser } from "./kubernetes/resource-browser";
import { KubernetesResourceDetail } from "./kubernetes/resource-detail";
import { KubernetesResourceWorkspace } from "./kubernetes/resource-workspace";
import { RedisKeyBrowser } from "./redis/key-browser";
import { RedisValueWorkspace } from "./redis/value-workspace";
import { S3ConnectorConsoleTemplate } from "./s3/console";

const styles = { border: "border", subtlePanel: "panel", muted: "muted", input: "input", rowHover: "hover", activeRow: "active" };

vi.mock("./s3/use-s3-browser", () => ({
  useS3Browser: () => ({ activeSession: { active: false, startedAt: "" } }),
}));
vi.mock("./s3/use-s3-upload", () => ({ useS3Upload: () => ({}) }));
vi.mock("./s3/use-s3-object-delete", () => ({ useS3ObjectDelete: () => ({}) }));

beforeEach(() => vi.clearAllMocks());

it("drives the split Kafka browser and detail controls", async () => {
  const user = userEvent.setup();
  const browser = {
    product: "Kafka",
    view: "topics",
    query: "",
    filteredItems: [{ name: "events", partition_count: 3, replication_factor: 2 }],
    selectedName: "events",
    activeDetail: { partitions: [{ partition: 0 }] },
    messages: [{ offset: 4 }],
    readForm: { partition: "0", start_position: "recent", max_records: "10", offset: "0" },
    state: { state: "idle", error: "", message: "" },
    changeView: vi.fn(),
    setQuery: vi.fn(),
    refreshList: vi.fn(),
    selectItem: vi.fn(),
    setReadForm: vi.fn(),
    readMessages: vi.fn(),
  };
  const writes = { openPublishDialog: vi.fn(), openOffsetDialog: vi.fn(), offsetPartitions: [] };
  const { rerender } = render(
    <>
      <KafkaResourceBrowser browser={browser} styles={styles} />
      <KafkaResourceDetail browser={browser} writes={writes} styles={styles} />
    </>,
  );

  await user.click(screen.getByRole("tab", { name: "Groups" }));
  await user.type(screen.getByRole("textbox", { name: "Filter topics" }), "event");
  await user.click(screen.getByRole("button", { name: /events/ }));
  await user.click(screen.getByRole("button", { name: "Publish" }));
  await user.click(screen.getByRole("button", { name: "Read" }));
  expect(browser.changeView).toHaveBeenCalledWith("groups");
  expect(browser.setQuery).toHaveBeenCalled();
  expect(browser.selectItem).toHaveBeenCalledWith(browser.filteredItems[0]);
  expect(writes.openPublishDialog).toHaveBeenCalledOnce();
  expect(browser.readMessages).toHaveBeenCalledOnce();

  rerender(<KafkaResourceBrowser browser={{ ...browser, filteredItems: [], state: { state: "loading" } }} styles={styles} />);
  expect(screen.getByText("Loading topics...")).toBeVisible();
});

it("drives the split Kubernetes browser, detail, and workspace", async () => {
  const user = userEvent.setup();
  const pod = { name: "api-1", namespace: "default", status: "Running", ready: "1/1", restarts: 0 };
  const browser = {
    tab: "pods",
    activeTab: { label: "Pods" },
    namespace: "",
    namespaces: [{ name: "default" }],
    filter: "",
    activeResources: [pod],
    filteredResources: [pod],
    selectedKey: "default/api-1",
    selectedResource: pod,
    latestAction: { status: "completed", action_name: "get_logs" },
    state: { state: "idle" },
    viewMode: "detail",
    detail: { output: { resource: pod } },
    logs: "ready",
    resultSearch: "",
    selectedPodConsoleLive: false,
    consolePending: false,
    refreshResource: vi.fn(),
    switchTab: vi.fn(),
    changeNamespace: vi.fn(),
    setFilter: vi.fn(),
    selectResource: vi.fn(),
    setResultSearch: vi.fn(),
    readLogs: vi.fn(),
    openPodConsole: vi.fn(),
    startPodConsole: vi.fn(),
  };
  const restart = { open: vi.fn() };
  render(
    <>
      <KubernetesResourceBrowser browser={browser} styles={styles} theme="dark" />
      <KubernetesResourceWorkspace browser={browser} restart={restart} target={{}} theme="dark" session={{}} styles={styles} />
    </>,
  );

  await user.click(screen.getByRole("tab", { name: "Nodes" }));
  await user.selectOptions(screen.getByRole("combobox", { name: "Kubernetes namespace" }), "default");
  await user.type(screen.getByRole("textbox", { name: "Filter pods" }), "api");
  await user.click(screen.getAllByRole("button", { name: /api-1/ })[0]);
  await user.click(screen.getByRole("button", { name: "Logs" }));
  await user.click(screen.getByRole("button", { name: "Open live console inside this pod" }));
  expect(browser.switchTab).toHaveBeenCalledWith("nodes");
  expect(browser.changeNamespace).toHaveBeenCalledWith("default");
  expect(browser.readLogs).toHaveBeenCalledWith(pod);
  expect(browser.openPodConsole).toHaveBeenCalledWith(pod);

  const { rerender } = render(
    <KubernetesResourceDetail
      tab="nodes"
      resource={{ name: "worker-1", status: "Ready" }}
      detail={null}
      logs=""
      search=""
      onSearch={vi.fn()}
      inputClass=""
      mutedClass=""
    />,
  );
  expect(screen.getByText(/do not expose pod-style logs/i)).toBeVisible();
  rerender(
    <KubernetesResourceDetail tab="pods" resource={null} detail={null} logs="" search="" onSearch={vi.fn()} inputClass="" mutedClass="" />,
  );
  expect(screen.getByText(/Select a Kubernetes resource/i)).toBeVisible();
});

it("drives the split Redis key and value surfaces", async () => {
  const user = userEvent.setup();
  const browser = {
    product: "Redis",
    keys: ["user:1"],
    selectedKeys: [],
    selectedCount: 0,
    activeKey: "user:1",
    cursor: "0",
    pattern: "*",
    latestAction: null,
    state: { state: "idle", error: "", message: "" },
    resultMode: "value",
    creatingKey: false,
    keyResult: { type: "string", value: "hello", ttl: 60 },
    ttlDraft: "60",
    canUpdateTTL: true,
    canSaveString: true,
    editableString: true,
    valueDraft: "hello",
    newKey: "",
    newValue: "",
    setPattern: vi.fn(),
    scanKeys: vi.fn(),
    startNewKey: vi.fn(),
    canStartNewKey: true,
    setSelectedKeys: vi.fn(),
    loadKey: vi.fn(),
    toggleSelection: vi.fn(),
    deleteSelected: vi.fn(),
    setResultMode: vi.fn(),
    setTTLDraft: vi.fn(),
    updateTTL: vi.fn(),
    saveStringValue: vi.fn(),
    setValueDraft: vi.fn(),
  };
  const view = render(
    <>
      <RedisKeyBrowser browser={browser} styles={styles} />
      <RedisValueWorkspace browser={browser} styles={styles} />
    </>,
  );
  await user.click(screen.getByRole("button", { name: /user:1/ }));
  await user.click(screen.getByRole("button", { name: "New" }));
  await user.click(screen.getByRole("button", { name: "Refresh keys" }));
  await user.click(screen.getByRole("button", { name: "All" }));
  await user.type(screen.getByRole("textbox", { name: "Redis key scan pattern" }), "cache:*");
  await user.click(screen.getByRole("button", { name: "Scan keys" }));
  await user.click(screen.getByRole("button", { name: "Raw JSON" }));
  await user.click(screen.getByRole("button", { name: "Save TTL" }));
  await user.click(screen.getByRole("button", { name: /Save string/ }));
  expect(browser.loadKey).toHaveBeenCalledWith("user:1");
  expect(browser.startNewKey).toHaveBeenCalledOnce();
  expect(browser.scanKeys).toHaveBeenCalledWith({ reset: true });
  expect(browser.setSelectedKeys).toHaveBeenCalledWith(["user:1"]);
  expect(browser.setPattern).toHaveBeenCalled();
  expect(browser.setResultMode).toHaveBeenCalledWith("json");
  expect(browser.updateTTL).toHaveBeenCalledOnce();
  expect(browser.saveStringValue).toHaveBeenCalledOnce();

  view.rerender(<RedisKeyBrowser browser={{ ...browser, canStartNewKey: false }} styles={styles} />);
  expect(screen.getByRole("button", { name: "New" })).toBeDisabled();
});

it("renders the split S3 console empty-session contract", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  render(
    <S3ConnectorConsoleTemplate
      target={{ ref: "s3:1:1", config: { host: "s3.example", bucket: "docs" } }}
      approvals={[]}
      theme="dark"
      session={{ active: false }}
      onNewStructuredSession={onStart}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Start S3 session" }));
  expect(onStart).toHaveBeenCalledOnce();
  expect(screen.getByText(/s3.example/)).toBeVisible();
});
