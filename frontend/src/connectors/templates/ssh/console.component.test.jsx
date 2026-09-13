import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { SSHConnectorToolbarActionsTemplate } from "./console";
import * as model from "./model";
import { SSHConnectorOperationsTemplate } from "./operations";

vi.mock("../../../lib/api", async (importOriginal) => ({ ...(await importOriginal()), apiPost: vi.fn() }));
vi.mock("./model", async (importOriginal) => ({ ...(await importOriginal()), resumeHostKeyAction: vi.fn() }));

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
    expect(apiPost).toHaveBeenCalledWith("/api/connectors/ssh/host-keys/approve", {
      host: "host.example",
      port: 22,
      public_key: "ssh-ed25519 test-key",
      replace: false,
    }),
  );
  expect(model.resumeHostKeyAction).toHaveBeenCalledWith(action);
  expect(onOperationComplete).toHaveBeenCalledWith({ status: "completed" }, { connector_kind: "ssh", ...action });
});
