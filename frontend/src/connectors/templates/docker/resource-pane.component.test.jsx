import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DockerLifecycleDialog } from "./lifecycle-dialog";
import { DockerResourcePane } from "./resource-pane";

const classes = { border: "border", muted: "muted", subtlePanel: "subtle", input: "input" };
const container = { id: "one", name: "api", image: "example/api", state: "running", status: "Up" };

function renderPane(overrides = {}) {
  const callbacks = {
    onTailChange: vi.fn(),
    onResultSearch: vi.fn(),
    onReadLogs: vi.fn(),
    onInspect: vi.fn(),
    onOpenConsole: vi.fn(),
    onStartConsole: vi.fn(),
    onEndConsole: vi.fn(),
    onLifecycle: vi.fn(),
  };
  render(
    <DockerResourcePane
      resourceView="containers"
      selectedResource={container}
      selectedContainer={container}
      containerRef="api"
      viewMode="logs"
      result={null}
      resultSearch=""
      tail={200}
      state={{ state: "idle", error: "" }}
      target={{ ref: "docker:1:1" }}
      selectedRuntimeTarget={null}
      session={null}
      sessionLive={false}
      consolePending={false}
      theme="dark"
      classes={classes}
      {...callbacks}
      {...overrides}
    />,
  );
  return callbacks;
}

it("routes Docker detail toolbar controls without owning action behavior", async () => {
  const user = userEvent.setup();
  const callbacks = renderPane();
  await user.click(screen.getByTitle("Inspect container"));
  await user.click(screen.getByTitle("Refresh logs"));
  await user.click(screen.getByTitle("Open live console inside this container"));
  await user.click(screen.getByTitle("Start container"));
  await user.click(screen.getByTitle("Stop container"));
  await user.click(screen.getByTitle("Restart container"));
  fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "50" } });

  expect(callbacks.onInspect).toHaveBeenCalledOnce();
  expect(callbacks.onReadLogs).toHaveBeenCalledOnce();
  expect(callbacks.onOpenConsole).toHaveBeenCalledOnce();
  expect(callbacks.onLifecycle.mock.calls.map(([action]) => action)).toEqual(["start_container", "stop_container", "restart_container"]);
  expect(callbacks.onTailChange).toHaveBeenCalledWith("50");
});

it("shows stable loading feedback for the selected Docker container", () => {
  renderPane({ state: { state: "loading", error: "" } });
  expect(screen.getByText("Loading logs for api...")).toBeInTheDocument();
});

it("keeps Docker lifecycle confirmation controlled by its owner", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(
    <DockerLifecycleDialog
      dialog={{
        open: true,
        title: "Stop Docker container",
        description: "This will stop the selected container.",
        details: [{ label: "Container", value: "api" }],
        pending: false,
      }}
      onClose={onClose}
      onConfirm={onConfirm}
    />,
  );
  expect(screen.getByText("api")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Run action" }));
  expect(onConfirm).toHaveBeenCalledOnce();
});
