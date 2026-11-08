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
