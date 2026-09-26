import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DockerContainerConsolePanel, dockerConsoleSessionName } from "./container-console-panel";

it("preserves Docker session identity and the pending/live terminal surfaces", async () => {
  const user = userEvent.setup();
  const onEnd = vi.fn();
  const props = {
    target: { ref: "docker:4:7" },
    container: { id: "api-id", name: "api" },
    containerRef: "api-id",
    selectedRuntimeTarget: { runtime_id: "opaque-runtime" },
    session: { id: 12, name: "docker:4:7:other-id" },
    sessionLive: false,
    pending: true,
    theme: "dark" as const,
    mutedClass: "",
    borderClass: "",
    onEnd,
  };
  const view = render(<DockerContainerConsolePanel {...props}>live terminal</DockerContainerConsolePanel>);
  expect(screen.getByText("Connecting container console")).toBeVisible();
  expect(screen.queryByText(/No active/)).not.toBeInTheDocument();
  view.rerender(
    <DockerContainerConsolePanel {...props} pending={false}>
      live terminal
    </DockerContainerConsolePanel>,
  );
  expect(screen.getByText(/Starting this console will close/)).toBeVisible();
  view.rerender(
    <DockerContainerConsolePanel {...props} sessionLive pending={false}>
      live terminal
    </DockerContainerConsolePanel>,
  );
  expect(screen.getByText("live terminal")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "End" }));
  expect(onEnd).toHaveBeenCalledOnce();
  expect(dockerConsoleSessionName(props.target, "api-id")).toBe("docker:docker:4:7:api-id");
  expect(dockerConsoleSessionName(null, "api-id")).toBe("docker:target:api-id");
});

it("requires a selected container and runtime before starting a console", () => {
  const props = { containerRef: "api", sessionLive: false, pending: false, theme: "light" as const, mutedClass: "", borderClass: "" };
  const view = render(<DockerContainerConsolePanel {...props} container={null} />);
  expect(screen.getByText(/Select a container/)).toBeVisible();
  view.rerender(<DockerContainerConsolePanel {...props} container={{ name: "api" }} />);
  expect(screen.getByRole("button", { name: "Start Container Console" })).toBeDisabled();
});
