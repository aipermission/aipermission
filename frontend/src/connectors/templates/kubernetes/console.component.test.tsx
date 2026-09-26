import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import type { ComponentType } from "react";
import type { ConsoleWorkspaceSlotProps } from "../../../components/console/console-workspace-types";
import { connectorActionFixture, connectorActionRequest } from "../../../test/connector-action-fixtures";
import { consoleWorkspaceFixture } from "../../../test/console-workspace-fixtures";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { apiPost as realPost } from "../../../lib/api";
import { KubernetesConnectorConsoleTemplate } from "./console";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));
const apiPost = vi.mocked(realPost);
const Console: ComponentType<ConsoleWorkspaceSlotProps> = KubernetesConnectorConsoleTemplate;

function propsFor(session: ConsoleWorkspaceSlotProps["session"] = null) {
  return consoleWorkspaceFixture({
    target: gatewayTargetFixture({
      connector_kind: "kubernetes",
      ref: "kubernetes:3:11",
      runtime_id: 91,
      config: { transport_target_ref: "transport:4:7" },
    }),
    session,
    selectedRuntimeTarget: { id: 91, name: "Kubernetes runtime" },
    selectedSessionLive: true,
    children: <p>Live pod terminal child</p>,
  });
}

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, value) => {
    const action = connectorActionRequest(value);
    const output =
      action.action_name === "list_namespaces"
        ? { namespaces: [{ name: "app" }] }
        : action.action_name === "list_pods"
          ? { pods: [{ name: "api-pod", namespace: "app", phase: "Running" }] }
          : action.action_name === "get_logs"
            ? { logs: "logs" }
            : { workloads: [] };
    return connectorActionFixture({ target_ref: action.target_ref, connector_kind: "kubernetes", action_name: action.action_name, output });
  });
});

async function selectPod() {
  const user = userEvent.setup();
  await user.click(screen.getByRole("tab", { name: "Pods" }));
  await user.click(await screen.findByRole("button", { name: /api-pod/ }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Open live console inside this pod" })).toBeEnabled());
  await user.click(screen.getByRole("button", { name: "Open live console inside this pod" }));
  return user;
}

it("uses the shared workspace session identity to retain a live pod terminal", async () => {
  const props = propsFor({ id: 9, name: "kubernetes:kubernetes:3:11:app:api-pod" });
  render(<Console {...props} />);
  const user = await selectPod();
  expect(props.onSelectLiveSessionName).toHaveBeenCalledWith("kubernetes:kubernetes:3:11:app:api-pod");
  expect(screen.getByText("Live pod terminal child")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "End" }));
  expect(props.onEndLiveSession).toHaveBeenCalledOnce();
  expect(props.onNewLiveSession).not.toHaveBeenCalled();
  expect(screen.getByText("transport:4:7")).toBeInTheDocument();
});

it("keeps structured state separate and forwards connector-owned pod start parameters", async () => {
  const props = propsFor({ active: true, startedAt: "now" });
  render(<Console {...props} />);
  const user = await selectPod();
  expect(screen.getByText("No active pod console")).toBeInTheDocument();
  expect(screen.queryByText("Live pod terminal child")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Start Pod Console" }));
  expect(props.onNewLiveSession).toHaveBeenCalledWith({
    name: "kubernetes:kubernetes:3:11:app:api-pod",
    params: { namespace: "app", pod: "api-pod" },
    closeExisting: false,
  });
});
