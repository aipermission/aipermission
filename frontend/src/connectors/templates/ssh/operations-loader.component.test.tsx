import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { SSHConnectorOperationsLoader } from "./operations-loader";
import type { SSHOperationProps } from "./operation-types";

const loading = vi.hoisted(() => {
  let release!: () => void;
  const ready = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { ready, release, calls: vi.fn(), workspace: "workspace-A" };
});
vi.mock("../../../lib/api", () => ({ currentWorkspaceBinding: () => loading.workspace }));
vi.mock("./operations", async () => {
  await loading.ready;
  return {
    SSHConnectorOperationsTemplate(props: SSHOperationProps) {
      loading.calls(props);
      return <div>Loaded SSH operation</div>;
    },
  };
});

it("does not load an inactive or foreign connector operation, and keeps lazy loading cancelable", async () => {
  const props: SSHOperationProps = { value: null, credentials: [], onChange: vi.fn() };
  const view = render(<SSHConnectorOperationsLoader {...props} />);
  const drifted = render(<SSHConnectorOperationsLoader {...props} value={{ open: true, connector_kind: "ssh", type: "key-cleanup" }} />);
  expect(await screen.findByRole("status")).toHaveTextContent("Loading operation...");
  loading.workspace = "workspace-B";
  drifted.rerender(<SSHConnectorOperationsLoader {...props} value={{ open: true, connector_kind: "ssh", type: "host-key" }} />);
  expect(props.onChange).toHaveBeenCalledWith({ open: false });
  expect(loading.calls).not.toHaveBeenCalled();
  drifted.unmount();
  vi.mocked(props.onChange).mockClear();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(loading.calls).not.toHaveBeenCalled();
  view.rerender(<SSHConnectorOperationsLoader {...props} value={{ open: true, connector_kind: "postgres" }} />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  const operation = { open: true, connector_kind: "ssh", type: "key-cleanup" };
  view.rerender(<SSHConnectorOperationsLoader {...props} value={operation} />);
  expect(await screen.findByRole("status")).toHaveTextContent("Loading operation...");
  await userEvent.setup().click(screen.getByRole("button", { name: "Close dialog" }));
  expect(props.onChange).toHaveBeenCalledWith({ open: false });
  view.rerender(<SSHConnectorOperationsLoader {...props} value={{ ...operation, open: false }} />);
  const pending = render(<SSHConnectorOperationsLoader {...props} value={operation} />);
  loading.workspace = "workspace-C";
  await act(async () => loading.release());
  await vi.waitFor(() => expect(props.onChange).toHaveBeenCalledWith({ open: false }));
  expect(loading.calls).not.toHaveBeenCalled();
  pending.unmount();
  view.rerender(<SSHConnectorOperationsLoader {...props} value={operation} />);
  expect(await screen.findByText("Loaded SSH operation")).toBeVisible();
  expect(loading.calls).toHaveBeenLastCalledWith({ ...props, value: operation });
});
