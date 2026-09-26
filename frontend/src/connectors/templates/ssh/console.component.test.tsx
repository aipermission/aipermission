import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost as realPost } from "../../../lib/api";
import { SSHConnectorToolbarActionsTemplate } from "./console";
import * as realModel from "./model";
import { SSHConnectorOperationsTemplate } from "./operations";
import type { SSHOperation, SSHDockerResponse } from "./operation-types";
import type { SSHConsoleRuntime, SSHToolbarProps } from "./console-types";
import type { ComponentType } from "react";
import type { ConsoleToolbarSlotProps } from "../../../components/console/console-workspace-types";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
const apiPost = vi.mocked(realPost);
const model = vi.mocked(realModel);

vi.mock("../../../lib/api", async () => ({
  ...(await vi.importActual<typeof import("../../../lib/api")>("../../../lib/api")),
  apiPost: vi.fn(),
}));
vi.mock("./model", async () => ({
  ...(await vi.importActual<typeof import("./model")>("./model")),
  checkDocker: vi.fn(),
  readDockerLogs: vi.fn(),
  resumeHostKeyAction: vi.fn(),
}));

vi.mock("../../../components/file-transfer/file-transfer-dialog", () => ({
  FileTransferDialog: ({ open, runtimeTarget }: { open: boolean; runtimeTarget?: { id: number } | null }) =>
    open ? <p data-testid="transfer-runtime">{runtimeTarget?.id || "none"}</p> : null,
}));
vi.mock("./bulk-command-dialog", () => ({
  BulkCommandDialog: ({ open, targets }: { open: boolean; targets: SSHConsoleRuntime[] }) =>
    open ? <p data-testid="bulk-targets">{targets.map((target) => target.connector_kind).join(",")}</p> : null,
}));

function toolbarProps(overrides: Partial<SSHToolbarProps> = {}): SSHToolbarProps {
  return {
    theme: "dark",
    selectedRuntimeTarget: null,
    selectedSession: {},
    selectedSessionLive: false,
    liveConsoleTargets: [],
    ...overrides,
  };
}

function deferred() {
  let resolve!: (_value: SSHDockerResponse) => void;
  let reject!: (_error: unknown) => void;
  const promise = new Promise<SSHDockerResponse>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function OperationsHarness({ initialValue }: { initialValue: SSHOperation }) {
  const [value, setValue] = useState(initialValue);
  return <SSHConnectorOperationsTemplate value={value} credentials={[]} onChange={setValue} />;
}

beforeEach(() => {
  apiPost.mockReset();
  model.checkDocker.mockReset();
  model.readDockerLogs.mockReset();
  model.resumeHostKeyAction.mockReset();
});

it("forwards cancellation ownership to SSH Docker operation requests", async () => {
  const actualModel = await vi.importActual<typeof import("./model")>("./model");
  const controller = new AbortController();
  const target = { id: 7 };
  const profile = { id: 11 };
  apiPost.mockResolvedValue({ ok: true });

  await actualModel.checkDocker({ target, profile, signal: controller.signal });
  await actualModel.readDockerLogs({
    target,
    profile,
    container: { id: "container-1", name: "api" },
    tail: 25,
    signal: controller.signal,
  });

  expect(apiPost).toHaveBeenNthCalledWith(
    1,
    "/api/connector-targets/7/operations/docker-check",
    { profile_id: 11 },
    { signal: controller.signal },
  );
  expect(apiPost).toHaveBeenNthCalledWith(
    2,
    "/api/connector-targets/7/operations/docker-logs",
    { profile_id: 11, container_ref: "container-1", tail: 25 },
    { signal: controller.signal },
  );
});

it("shows only SSH runtimes in Bulk and requires the file-transfer surface for Files", async () => {
  const user = userEvent.setup();
  const sshTarget = {
    id: 7,
    connector_kind: "ssh",
    target: { transfer_runtime_id: 17 },
    username: "root",
    host: "host.example",
    port: 22,
  };
  const dockerTarget = { id: 8, connector_kind: "docker", target: {} };
  const view = render(
    <SSHConnectorToolbarActionsTemplate
      {...toolbarProps({ selectedRuntimeTarget: sshTarget, liveConsoleTargets: [sshTarget, dockerTarget] })}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Bulk" }));
  expect(screen.getByTestId("bulk-targets")).toHaveTextContent("ssh");
  await user.click(screen.getByRole("button", { name: "Files" }));
  expect(screen.getByTestId("transfer-runtime")).toHaveTextContent("17");

  view.rerender(
    <SSHConnectorToolbarActionsTemplate
      {...toolbarProps({ selectedRuntimeTarget: { ...sshTarget, target: {} }, liveConsoleTargets: [sshTarget] })}
    />,
  );
  expect(screen.getByRole("button", { name: "Files" })).toBeDisabled();
});

it("accepts the shared toolbar slot and preserves generic session actions", async () => {
  const Toolbar: ComponentType<ConsoleToolbarSlotProps> = SSHConnectorToolbarActionsTemplate;
  const selectedTarget = gatewayTargetFixture({ connector_kind: "ssh", transfer_runtime_id: 17 });
  const props: ConsoleToolbarSlotProps = {
    theme: "dark",
    selectedTarget,
    selectedRuntimeTarget: {
      id: 7,
      name: "Example",
      connector_kind: "ssh",
      target: selectedTarget,
      username: "root",
      host: "host.test",
      port: 22,
    },
    selectedSession: { id: 9, status: "connected" },
    selectedSessionLive: true,
    selectedUnreadMessages: [],
    liveConsoleTargets: [],
    structuredSession: null,
    onOpenMessages: vi.fn(),
    onRefreshSessions: vi.fn(),
    onNewSession: vi.fn(),
    onEndSession: vi.fn(),
    onInterrupt: vi.fn(),
    onNewStructuredSession: vi.fn(),
    onEndStructuredSession: vi.fn(),
  };
  render(<Toolbar {...props} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Messages" }));
  await user.click(screen.getByRole("button", { name: "New Session" }));
  await user.click(screen.getByRole("button", { name: "End Session" }));
  await user.click(screen.getByRole("button", { name: "Interrupt" }));
  expect(props.onOpenMessages).toHaveBeenCalledOnce();
  expect(props.onNewSession).toHaveBeenCalledOnce();
  expect(props.onEndSession).toHaveBeenCalledOnce();
  expect(props.onInterrupt).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Files" }));
  expect(screen.getByTestId("transfer-runtime")).toHaveTextContent("17");
});

it("approves SSH host fingerprints through the connector-owned route", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const onOperationComplete = vi.fn();
  const action = {
    type: "test" as const,
    kind: "ssh" as const,
    target: { id: 7, name: "Example", connector_kind: "ssh" },
    profile: { id: 11 },
  };
  apiPost.mockResolvedValue({});
  model.resumeHostKeyAction.mockResolvedValue({ message: "Connector updated." });

  render(
    <SSHConnectorOperationsTemplate
      value={{
        open: true,
        connector_kind: "ssh",
        type: "host-key",
        state: "idle",
        hostKey: {
          host: "host.example",
          hostname: "host.example:22",
          port: 22,
          public_key: "ssh-ed25519 test-key",
          fingerprint_sha256: "SHA256:test",
          key_type: "ssh-ed25519",
          changed: false,
        },
        action,
      }}
      credentials={[]}
      onChange={onChange}
      onOperationComplete={onOperationComplete}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Approve fingerprint" }));

  await waitFor(() =>
    expect(apiPost).toHaveBeenCalledWith(
      "/api/connectors/ssh/host-keys/approve",
      {
        host: "host.example",
        port: 22,
        public_key: "ssh-ed25519 test-key",
        replace: false,
      },
      { signal: expect.any(AbortSignal) },
    ),
  );
  expect(model.resumeHostKeyAction).toHaveBeenCalledWith(action);
  expect(onOperationComplete).toHaveBeenCalledWith({ message: "Connector updated." }, { connector_kind: "ssh", ...action });
});

it.each([
  ["success", (pending: ReturnType<typeof deferred>) => pending.resolve({ available: true, ok: true, containers: [] })],
  ["failure", (pending: ReturnType<typeof deferred>) => pending.reject(new Error("status unavailable"))],
])("keeps a dismissed Docker status dialog closed after late %s", async (_outcome, settle) => {
  const user = userEvent.setup();
  const pending = deferred();
  model.checkDocker.mockReturnValue(pending.promise);
  render(
    <OperationsHarness
      initialValue={{
        open: true,
        connector_kind: "ssh",
        type: "docker-check",
        state: "idle",
        target: { id: 7, name: "Example host", connector_kind: "ssh" },
        profile: { id: 11 },
      }}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Close dialog" }));
  await act(async () => settle(pending));

  expect(screen.queryByRole("dialog", { name: "Docker on Example host" })).not.toBeInTheDocument();
});

it("keeps dismissed Docker logs closed after a late refresh", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  model.readDockerLogs.mockReturnValue(pending.promise);
  render(
    <OperationsHarness
      initialValue={{
        open: true,
        connector_kind: "ssh",
        type: "docker-logs",
        state: "ready",
        target: { id: 7, name: "Example host", connector_kind: "ssh" },
        profile: { id: 11 },
        container: { id: "container-1", name: "api" },
        data: { ok: true, stdout: "old", stderr: "", exit_code: 0, duration_ms: 1 },
      }}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await user.click(screen.getByRole("button", { name: "Close dialog" }));
  await act(async () => pending.resolve({ ok: true, stdout: "late", stderr: "", exit_code: 0, duration_ms: 2 }));

  expect(screen.queryByRole("dialog", { name: "api logs" })).not.toBeInTheDocument();
});

it("surfaces malformed Docker metadata as a safe operation error", async () => {
  model.checkDocker.mockResolvedValue({ ok: true, available: true, containers: [{ ports: {} }] });
  render(
    <OperationsHarness
      initialValue={{
        open: true,
        connector_kind: "ssh",
        type: "docker-check",
        state: "idle",
        target: { id: 7, name: "Example", connector_kind: "ssh" },
        profile: { id: 11 },
      }}
    />,
  );
  expect(await screen.findByText("Invalid SSH Docker operation response.")).toBeVisible();
});
