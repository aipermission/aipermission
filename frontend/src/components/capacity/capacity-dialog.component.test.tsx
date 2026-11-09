import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { CapacityDialog } from "./capacity-dialog";
import { CapacityButton } from "./capacity-button";
import { report } from "../../test/console-capacity-fixtures";

it("shows real sidebar usage and green/amber/red thresholds", () => {
  const props = { data: report(), error: null, onOpen: vi.fn() };
  const view = render(<CapacityButton {...props} />);
  expect(screen.getByText("3 / 32")).toBeVisible();
  expect(screen.getByRole("progressbar")).toHaveValue((3 / 32) * 100);
  expect(screen.getByRole("progressbar")).toHaveAttribute("data-tone", "normal");
  view.rerender(<CapacityButton {...props} data={{ ...report(), used: 25 }} />);
  expect(screen.getByRole("progressbar")).toHaveAttribute("data-tone", "warning");
  view.rerender(<CapacityButton {...props} data={{ ...report(), used: 30 }} />);
  expect(screen.getByRole("progressbar")).toHaveAttribute("data-tone", "critical");
});

it("defaults to 30m, selects only idle candidates, confirms and submits the exact snapshot", async () => {
  const user = userEvent.setup();
  const onClean = vi.fn(async () => {});
  render(
    <CapacityDialog data={report()} error={null} message={null} saving={false} onClean={onClean} onClose={vi.fn()} onRefresh={vi.fn()} />,
  );
  expect(screen.getByLabelText("Idle threshold")).toHaveValue("30");
  expect(screen.getByRole("checkbox", { name: "Select session 1" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "Select session 3" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close session 3" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Clean 1 sessions" }));
  expect(onClean).not.toHaveBeenCalled();
  const confirmation = screen.getByRole("dialog", { name: "Close 1 console sessions?" });
  await user.click(within(confirmation).getByRole("button", { name: "Close sessions" }));
  expect(onClean).toHaveBeenCalledWith([report().items[0]], 30);
});

it("filters and sorts without fetching again and supports a separate single close", async () => {
  const user = userEvent.setup();
  const onClean = vi.fn(async () => {});
  render(
    <CapacityDialog data={report()} error={null} message={null} saving={false} onClean={onClean} onClose={vi.fn()} onRefresh={vi.fn()} />,
  );
  await user.selectOptions(screen.getByLabelText("Idle threshold"), "120");
  expect(screen.getByRole("button", { name: "Clean 0 sessions" })).toBeDisabled();
  await user.selectOptions(screen.getByLabelText("Session order"), "newest");
  expect(screen.getAllByRole("row")[1]).toHaveTextContent("Target 2");
  await user.click(screen.getByRole("tab", { name: /Running commands/ }));
  expect(screen.queryByText("Target 1")).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: /All sessions/ }));
  await user.click(screen.getByRole("button", { name: "Close session 2" }));
  await user.click(
    within(screen.getByRole("dialog", { name: "Close 1 console sessions?" })).getByRole("button", { name: "Close sessions" }),
  );
  expect(onClean).toHaveBeenCalledWith([report().items[1]], 0);
});

it("does not carry an explicit selection to another generation or activity snapshot", async () => {
  const user = userEvent.setup();
  const props = {
    data: report(),
    error: null,
    message: null,
    saving: false,
    onClean: vi.fn(async () => {}),
    onClose: vi.fn(),
    onRefresh: vi.fn(),
  };
  const view = render(<CapacityDialog {...props} />);
  await user.click(screen.getByRole("button", { name: "Select idle (1)" }));
  view.rerender(
    <CapacityDialog
      {...props}
      data={{ ...report(), items: report().items.map((item) => (item.id === 1 ? { ...item, generation: 2 } : item)) }}
    />,
  );
  expect(screen.getByRole("checkbox", { name: "Select session 1" })).not.toBeChecked();
  expect(screen.getByRole("button", { name: "Clean 0 sessions" })).toBeDisabled();
});

function renderSearchDialog(data = report()) {
  const props = { data, error: null, message: null, saving: false, onClean: vi.fn(async () => {}), onClose: vi.fn(), onRefresh: vi.fn() };
  const view = render(<CapacityDialog {...props} />);
  return { ...view, props, user: userEvent.setup(), search: screen.getByRole("searchbox", { name: "Search sessions" }) };
}

function expectVisibleSessions(ids: number[]) {
  expect(
    screen
      .getAllByRole("row")
      .slice(1)
      .map((row) => within(row).getByRole("checkbox").getAttribute("aria-label")),
  ).toEqual(ids.map((id) => `Select session ${id}`));
}

it.each([
  ["  tArGeT 1  ", [1]],
  ["sHeLl 2", [2]],
  ["MCP_TOKEN:1", [1, 3, 2]],
  ["DoCkEr", [1, 3, 2]],
  ["#3", [3]],
  ["2", [2]],
  ["   ", [1, 3, 2]],
] as const)("searches loaded session fields case-insensitively with %j", async (query, ids) => {
  const {
    user,
    search,
    props: { onRefresh, onClean },
  } = renderSearchDialog();
  await user.type(search, query);
  expectVisibleSessions([...ids]);
  expect(onRefresh).not.toHaveBeenCalled();
  expect(onClean).not.toHaveBeenCalled();
});

it("combines search with group, idle threshold and activity order without refreshing", async () => {
  const data = report();
  data.items = data.items.map((item) => ({ ...item, owner: item.id === 3 ? "other" : "developer" }));
  const {
    user,
    search,
    props: { onRefresh },
  } = renderSearchDialog(data);
  await user.type(search, "DEVELOPER");
  expectVisibleSessions([1, 2]);
  await user.selectOptions(screen.getByLabelText("Session order"), "newest");
  expectVisibleSessions([2, 1]);
  await user.click(screen.getByRole("tab", { name: /^Idle/ }));
  expectVisibleSessions([1]);
  await user.selectOptions(screen.getByLabelText("Idle threshold"), "120");
  expectVisibleSessions([]);
  await user.click(screen.getByRole("tab", { name: /^Connected/ }));
  expectVisibleSessions([2, 1]);
  await user.selectOptions(screen.getByLabelText("Idle threshold"), "30");
  expectVisibleSessions([2]);
  await user.clear(search);
  await user.click(screen.getByRole("tab", { name: /All sessions/ }));
  expectVisibleSessions([2, 3, 1]);
  expect(onRefresh).not.toHaveBeenCalled();
});

it("shows an empty search result without changing hidden idle selection or single-close safety", async () => {
  const {
    user,
    search,
    props: { onRefresh, onClean },
  } = renderSearchDialog();
  await user.type(search, "no matching session");
  expect(screen.getByText("No sessions match your search.")).toBeVisible();
  expectVisibleSessions([]);
  expect(screen.getByRole("button", { name: "Clean 1 sessions" })).toBeEnabled();
  await user.clear(search);
  expect(screen.queryByText("No sessions match your search.")).not.toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: "Select session 1" })).toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "Select session 1" }));
  await user.type(search, "#3");
  expect(screen.getByRole("button", { name: "Close session 3" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Clean 0 sessions" })).toBeDisabled();
  await user.clear(search);
  expect(screen.getByRole("checkbox", { name: "Select session 1" })).not.toBeChecked();
  expect(onRefresh).not.toHaveBeenCalled();
  expect(onClean).not.toHaveBeenCalled();
});

it("keeps the selected confirmation snapshot when search hides it and the report changes", async () => {
  const { user, search, rerender, props } = renderSearchDialog();
  await user.click(screen.getByRole("button", { name: "Select idle (1)" }));
  await user.type(search, "#2");
  await user.click(screen.getByRole("button", { name: "Clean 1 sessions" }));
  const changed = { ...report(), items: report().items.map((item) => ({ ...item, generation: 2, last_activity: "2026-11-08T10:00:00Z" })) };
  rerender(<CapacityDialog {...props} data={changed} />);
  const confirmation = screen.getByRole("dialog", { name: "Close 1 console sessions?" });
  expect(within(confirmation).getByRole("listitem")).toHaveTextContent("Target 1 / #1 / mcp_token:1");
  await user.click(within(confirmation).getByRole("button", { name: "Close sessions" }));
  expect(props.onClean).toHaveBeenCalledExactlyOnceWith([report().items[0]], 30);
});
