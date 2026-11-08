import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { report, session } from "../../test/console-capacity-fixtures";
import { CapacityButton } from "./capacity-button";

it.each([
  [0, "normal"],
  [22, "normal"],
  [23, "warning"],
  [29, "critical"],
  [32, "critical"],
] as const)("shows bounded native progress at %i sessions with %s tone", (used, tone) => {
  const data = { ...report(), used, available: 32 - used, items: Array.from({ length: used }, (_, i) => session(i + 1)) };
  render(<CapacityButton data={data} error={null} onOpen={vi.fn()} />);
  const progress = screen.getByRole("progressbar", { name: "Console capacity" });
  expect(progress.tagName).toBe("PROGRESS");
  expect(progress).toHaveValue((used / 32) * 100);
  expect(progress).toHaveAttribute("data-tone", tone);
  expect(progress).not.toHaveAttribute("style");
  expect(screen.getByRole("button")).toHaveTextContent(`${used} / 32`);
});

it("keeps the dialog reachable while loading or unavailable", async () => {
  const onOpen = vi.fn();
  const user = userEvent.setup();
  const view = render(<CapacityButton data={null} error={null} onOpen={onOpen} />);
  expect(screen.getByRole("button")).toHaveTextContent("Loading...");
  expect(screen.getByRole("progressbar")).toHaveValue(0);
  view.rerender(<CapacityButton data={null} error="Refresh required" onOpen={onOpen} />);
  expect(screen.getByRole("button")).toHaveTextContent("Unavailable");
  expect(screen.getByRole("progressbar")).toHaveAttribute("data-tone", "muted");
  await user.click(screen.getByRole("button"));
  expect(onOpen).toHaveBeenCalledOnce();
});
