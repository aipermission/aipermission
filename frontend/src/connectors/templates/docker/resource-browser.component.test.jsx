import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DockerResourceBrowser } from "./resource-browser";

const classes = { border: "border", muted: "muted", subtlePanel: "subtle", input: "input", rowHover: "hover", activeRow: "active" };

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
