import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { SSHConnectorToolbarActionsTemplate } from "./console";

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
