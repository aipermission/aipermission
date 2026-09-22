import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { SSHConnectorToolbarActionsTemplate } from "./console";
import * as model from "./model";
import { SSHConnectorOperationsTemplate } from "./operations";

vi.mock("../../../lib/api", async (importOriginal) => ({ ...(await importOriginal()), apiPost: vi.fn() }));
vi.mock("./model", async (importOriginal) => ({
  ...(await importOriginal()),
  checkDocker: vi.fn(),
  readDockerLogs: vi.fn(),
  resumeHostKeyAction: vi.fn(),
}));

vi.mock("../../../components/file-transfer/file-transfer-dialog", () => ({
  FileTransferDialog: ({ open, runtimeTarget }) => (open ? <p data-testid="transfer-runtime">{runtimeTarget?.id || "none"}</p> : null),
}));
vi.mock("./bulk-command-dialog", () => ({
  BulkCommandDialog: ({ open, targets }) =>
    open ? <p data-testid="bulk-targets">{targets.map((target) => target.connector_kind).join(",")}</p> : null,
}));

function toolbarProps(overrides = {}) {
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
  let resolve;
  let reject;
  const promise = new Promise((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function OperationsHarness({ initialValue }) {
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
  const actualModel = await vi.importActual("./model");
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

it("approves SSH host fingerprints through the connector-owned route", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const onOperationComplete = vi.fn();
  const action = { type: "resume", kind: "ssh", target: { id: 7 }, profile: { id: 11 } };
  apiPost.mockResolvedValue({});
  model.resumeHostKeyAction.mockResolvedValue({ status: "completed" });

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
  expect(onOperationComplete).toHaveBeenCalledWith({ status: "completed" }, { connector_kind: "ssh", ...action });
});

it.each([
  ["success", (pending) => pending.resolve({ available: true, ok: true, containers: [] })],
  ["failure", (pending) => pending.reject(new Error("status unavailable"))],
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
        target: { id: 7, name: "Example host" },
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
        target: { id: 7, name: "Example host" },
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
