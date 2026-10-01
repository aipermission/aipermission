import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { useLayoutEffect } from "react";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { RoutingKeyPicker } from "./routing-key-picker";
import { queueNameLabel } from "./helpers";

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

it("keeps pointer and keyboard selection on the same routing option", () => {
  const onQueue = vi.fn();
  render(
    <RoutingKeyPicker
      queues={[{ name: "jobs.ready" }]}
      value=""
      custom={false}
      onQueue={onQueue}
      onCustom={vi.fn()}
      styles={connectorConsoleTheme("dark")}
    />,
  );
  const input = screen.getByRole("combobox");
  fireEvent.focus(input);
  const option = screen.getByRole("option", { name: /jobs.ready/ });
  fireEvent.mouseEnter(option);
  expect(option).toHaveAttribute("aria-selected", "true");
  fireEvent.keyDown(input, { key: "Enter" });
  expect(onQueue).toHaveBeenCalledExactlyOnceWith("jobs.ready");
  expect(input).toHaveAttribute("aria-expanded", "false");
});

it.each(["jobs", " jobs ", " ", "  ", "\u00a0", "\t", '" jobs "', "\\u0020"])(
  "selects the exact collision-safe identity %j with pointer and keyboard",
  (name) => {
    const onQueue = vi.fn();
    render(
      <RoutingKeyPicker
        queues={["jobs", " jobs ", " ", "  ", "\u00a0", "\t", '" jobs "', "\\u0020", "jobs"].map((name) => ({ name }))}
        value={name}
        custom={false}
        onQueue={onQueue}
        onCustom={vi.fn()}
        styles={connectorConsoleTheme("dark")}
      />,
    );
    const input = screen.getByRole("combobox");
    expect(input).toHaveValue(name);
    fireEvent.focus(input);
    expect(screen.getAllByRole("option")).toHaveLength(9);
    const label = queueNameLabel(name);
    const option = screen.getByRole("option", { name: `${label} Queue routing key via amq.default` });
    fireEvent.mouseDown(option);
    expect(onQueue).toHaveBeenLastCalledWith(name);
    fireEvent.focus(input);
    const index = screen.getAllByRole("option").findIndex((item) => item.textContent?.startsWith(label));
    expect(index).toBeGreaterThan(0);
    for (let step = 0; step < index; step += 1) fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onQueue).toHaveBeenNthCalledWith(2, name);
    expect(input).toHaveAttribute("aria-expanded", "false");
  },
);

it("does not choose a removed queue before the refreshed list clamps its active option", async () => {
  const onQueue = vi.fn();
  const onCustom = vi.fn();
  function RefreshingPicker({ empty }: { empty: boolean }) {
    useLayoutEffect(() => {
      // A committed queue refresh may precede the picker's passive index update.
      if (empty) screen.getByRole("combobox").dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    }, [empty]);
    return (
      <RoutingKeyPicker
        queues={empty ? [] : [{ name: "jobs.ready" }]}
        value=""
        custom={false}
        onQueue={onQueue}
        onCustom={onCustom}
        styles={connectorConsoleTheme("dark")}
      />
    );
  }
  const view = render(<RefreshingPicker empty={false} />);
  const input = screen.getByRole("combobox");
  fireEvent.focus(input);
  fireEvent.keyDown(input, { key: "ArrowDown" });
  expect(input).toHaveAttribute("aria-activedescendant", "rabbit-routing-option-1");
  await act(async () => view.rerender(<RefreshingPicker empty />));
  expect(onQueue).not.toHaveBeenCalled();
  expect(onCustom).not.toHaveBeenCalled();
  expect(input).toHaveAttribute("aria-expanded", "true");
  expect(input).toHaveAttribute("aria-activedescendant", "rabbit-routing-option-0");
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
