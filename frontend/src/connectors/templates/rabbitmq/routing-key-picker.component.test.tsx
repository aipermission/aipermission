import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { RoutingKeyPicker } from "./routing-key-picker";

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
