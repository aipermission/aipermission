import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { KubernetesPodConsolePanel, kubernetesConsoleSessionName } from "./pod-console-panel";

it("keeps pod console pending feedback, identity, and start/end callbacks on the shared surface", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  const onEnd = vi.fn();
  const pod = { namespace: "apps", name: "api-1" };
  const props = { pod, selectedRuntimeTarget: { runtime_id: "opaque" }, sessionLive: false, pending: true, theme: "dark" as const, mutedClass: "", borderClass: "", onStart, onEnd };
  const view = render(<KubernetesPodConsolePanel {...props}>terminal</KubernetesPodConsolePanel>);
  expect(screen.getByText("Connecting pod console")).toBeVisible();
  expect(screen.queryByText(/No active/)).not.toBeInTheDocument();
  view.rerender(<KubernetesPodConsolePanel {...props} pending={false} />);
  await user.click(screen.getByRole("button", { name: "Start Pod Console" }));
  expect(onStart).toHaveBeenCalledOnce();
  view.rerender(<KubernetesPodConsolePanel {...props} sessionLive pending={false}>terminal</KubernetesPodConsolePanel>);
  expect(screen.getByText("terminal")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "End" }));
  expect(onEnd).toHaveBeenCalledOnce();
  expect(kubernetesConsoleSessionName({ ref: "kubectl:2:5" }, pod)).toBe("kubernetes:kubectl:2:5:apps:api-1");
  expect(kubernetesConsoleSessionName(null, null)).toBe("kubernetes:target:namespace:pod");
});

it("shows the pod selection placeholder without opening a console", () => {
  render(<KubernetesPodConsolePanel pod={null} sessionLive={false} pending={false} theme="light" mutedClass="" borderClass="" />);
  expect(screen.getByText(/Select a pod/)).toBeVisible();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
