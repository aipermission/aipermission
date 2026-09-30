import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { RoutingKeyPicker } from "./routing-key-picker";

afterEach(() => vi.useRealTimers());

function picker() {
  return render(
    <RoutingKeyPicker
      queues={[{ name: "jobs.ready" }]}
      value=""
      custom={false}
      onQueue={vi.fn()}
      onCustom={vi.fn()}
      styles={connectorConsoleTheme("dark")}
    />,
  );
}

it("closes on blur without leaving a delayed update after unmount", () => {
  vi.useFakeTimers();
  const view = picker();
  const input = screen.getByRole("combobox");
  fireEvent.focus(input);
  expect(screen.getByRole("listbox")).toBeVisible();
  fireEvent.blur(input);
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  expect(vi.getTimerCount()).toBe(0);
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it("does not let an earlier blur close a refocused picker", () => {
  vi.useFakeTimers();
  const view = picker();
  const input = screen.getByRole("combobox");
  fireEvent.focus(input);
  fireEvent.blur(input);
  fireEvent.focus(input);
  act(() => vi.advanceTimersByTime(150));
  expect(screen.getByRole("listbox")).toBeVisible();
  expect(input).toHaveAttribute("aria-expanded", "true");
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it("selects filtered queue names with keyboard navigation and supports custom routing", async () => {
  const user = userEvent.setup();
  const onQueue = vi.fn();
  const onCustom = vi.fn();
  render(
    <RoutingKeyPicker
      queues={[{ name: "jobs.ready" }, { name: "other" }]}
      value=""
      custom={false}
      onQueue={onQueue}
      onCustom={onCustom}
      styles={connectorConsoleTheme("dark")}
    />,
  );
  const input = screen.getByRole("combobox");
  await user.click(input);
  await user.type(input, "ready");
  expect(screen.queryByRole("option", { name: /other/ })).not.toBeInTheDocument();
  await user.keyboard("{ArrowDown}{Enter}");
  expect(onQueue).toHaveBeenCalledWith("jobs.ready");
  expect(input).toHaveAttribute("aria-expanded", "false");
  await user.tab();
  await user.click(input);
  await user.keyboard("{Tab}");
  expect(onCustom).toHaveBeenCalledOnce();
  await user.tab();
  await user.click(input);
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});
