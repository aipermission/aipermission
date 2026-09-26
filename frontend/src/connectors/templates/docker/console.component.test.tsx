import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import type { ComponentType } from "react";
import type { ConsoleWorkspaceSlotProps } from "../../../components/console/console-workspace-types";
import { connectorActionFixture, connectorActionRequest } from "../../../test/connector-action-fixtures";
import { consoleWorkspaceFixture } from "../../../test/console-workspace-fixtures";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { apiPost as realPost } from "../../../lib/api";
import { DockerConnectorConsoleTemplate } from "./console";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));
const apiPost = vi.mocked(realPost);
const Console: ComponentType<ConsoleWorkspaceSlotProps> = DockerConnectorConsoleTemplate;

function propsFor(session: ConsoleWorkspaceSlotProps["session"] = null) {
  return consoleWorkspaceFixture({
    target: gatewayTargetFixture({
      connector_kind: "docker",
      ref: "docker:3:11",
      runtime_id: 91,
      config: { transport_target_ref: "transport:4:7" },
    }),
    session,
    selectedRuntimeTarget: { id: 91, name: "Docker runtime" },
    selectedSessionLive: true,
    children: <p>Live terminal child</p>,
  });
}

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, value) => {
    const action = connectorActionRequest(value);
    return connectorActionFixture({
      target_ref: action.target_ref,
      connector_kind: "docker",
      action_name: action.action_name,
      output:
        action.action_name === "list_containers" ? { containers: [{ id: "api-id", name: "api", state: "running" }] } : { stdout: "logs" },
    });
  });
});

it("binds the shared runtime slot to the selected container's existing session and terminal child", async () => {
  const props = propsFor({ id: 9, name: "docker:docker:3:11:api" });
  render(<Console {...props} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: /^api running/ }));
  await waitFor(() => expect(screen.getByTitle("Open live console inside this container")).toBeEnabled());
  await user.click(screen.getByTitle("Open live console inside this container"));
  expect(props.onSelectLiveSessionName).toHaveBeenCalledWith("docker:docker:3:11:api");
  expect(screen.getByText("Live terminal child")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "End" }));
  expect(props.onEndLiveSession).toHaveBeenCalledOnce();
  expect(props.onNewLiveSession).not.toHaveBeenCalled();
  expect(screen.getByText("transport:4:7")).toBeInTheDocument();
});

it("does not confuse structured session state with a container console and forwards native start parameters", async () => {
  const props = propsFor({ active: true, startedAt: "now" });
  render(<Console {...props} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: /^api running/ }));
  await waitFor(() => expect(screen.getByTitle("Open live console inside this container")).toBeEnabled());
  await user.click(screen.getByTitle("Open live console inside this container"));
  expect(screen.getByText("No active container console")).toBeInTheDocument();
  expect(screen.queryByText("Live terminal child")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Start Container Console" }));
  expect(props.onNewLiveSession).toHaveBeenCalledWith({
    name: "docker:docker:3:11:api",
    params: { container: "api" },
    closeExisting: false,
  });
});
