import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DockerResourceBrowser } from "./resource-browser";
import type { DockerResource, DockerResourceKind } from "./resource-types";

const classes = { border: "border", muted: "muted", subtlePanel: "subtle", input: "input", rowHover: "hover", activeRow: "active" };

it.each<[DockerResourceKind, DockerResource, string]>([
  ["images", { id: "image-id", repository: "api", tag: "latest" }, "image-id"],
  ["networks", { id: "network-id", name: "apps" }, "network-id"],
  ["volumes", { name: "data" }, "data"],
])("selects %s resources by their own identity and preserves payloads", async (resourceView, resource, identity) => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  const props = { resourceView, items: [resource], visibleCount: 1, selectedContainer: null, selectedResourceID: identity, filter: "", state: { state: "loading" }, latestAction: null, theme: "dark", classes, onRefresh: vi.fn(), onSwitchView: vi.fn(), onFilter: vi.fn(), onSelect };
  const view = render(<DockerResourceBrowser {...props} />);
  const selected = screen.getByRole("button", { name: new RegExp(resource.repository || resource.name || identity), pressed: true });
  expect(selected).toHaveTextContent(resource.repository || resource.name || "");
  await user.click(selected);
  expect(onSelect).toHaveBeenCalledWith(resource);
  const refresh = screen.getByTitle(`Refresh ${resourceView}`);
  expect(refresh).toBeDisabled();
  await user.click(refresh);
  expect(props.onRefresh).not.toHaveBeenCalled();
  view.rerender(<DockerResourceBrowser {...props} selectedResourceID="other" state={{ state: "error" }} />);
  expect(selected).toHaveAttribute("aria-pressed", "false");
  expect(refresh).toBeEnabled();
});

it.each([{ id: "same-id", name: "different" }, { id: "different", name: "api" }])("matches selected containers independently by ID or name", (selectedContainer) => {
  render(<DockerResourceBrowser resourceView="containers" items={[{ id: "same-id", name: "api" }]} visibleCount={1} selectedContainer={selectedContainer} selectedResourceID="" filter="" state={{ state: "idle" }} latestAction={null} theme="light" classes={classes} onRefresh={vi.fn()} onSwitchView={vi.fn()} onFilter={vi.fn()} onSelect={vi.fn()} />);
  expect(screen.getByRole("button", { name: /api/i })).toHaveAttribute("aria-pressed", "true");
});

it("keeps resource selection, filtering, refresh, and tabs connector-owned", async () => {
  const user = userEvent.setup();
  const onRefresh = vi.fn();
  const onSwitchView = vi.fn();
  const onFilter = vi.fn();
  const onSelect = vi.fn();
  const container = { id: "one", name: "api", image: "example/api", state: "running", status: "Up" };
  render(
    <DockerResourceBrowser
      resourceView="containers"
      items={[container]}
      visibleCount={1}
      selectedContainer={container}
      selectedResourceID=""
      filter=""
      state={{ state: "idle" }}
      latestAction={null}
      theme="dark"
      classes={classes}
      onRefresh={onRefresh}
      onSwitchView={onSwitchView}
      onFilter={onFilter}
      onSelect={onSelect}
    />,
  );
  expect(screen.getByRole("button", { name: /api/i })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "Containers" })).toHaveAttribute("aria-pressed", "true");
  await user.click(screen.getByTitle("Refresh containers"));
  await user.click(screen.getByRole("button", { name: "Images" }));
  await user.type(screen.getByPlaceholderText("Search containers"), "a");
  await user.click(screen.getByRole("button", { name: /api/i }));
  expect(onRefresh).toHaveBeenCalledOnce();
  expect(onSwitchView).toHaveBeenCalledWith("images");
  expect(onFilter).toHaveBeenCalledWith("a");
  expect(onSelect).toHaveBeenCalledWith(container);
});

it("renders empty light-theme resource views and failed action state", () => {
  render(
    <DockerResourceBrowser
      resourceView="images"
      items={[]}
      visibleCount={0}
      selectedContainer={null}
      selectedResourceID=""
      filter=""
      state={{ state: "idle" }}
      latestAction={{ status: "failed", action_name: "list_images" }}
      theme="light"
      classes={classes}
      onRefresh={vi.fn()}
      onSwitchView={vi.fn()}
      onFilter={vi.fn()}
      onSelect={vi.fn()}
    />,
  );
  expect(screen.getByText("list_images")).toBeVisible();
  expect(screen.getByText(/No images matched/i)).toBeVisible();
});
