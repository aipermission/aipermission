import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { renderSidebar } from "../test/fixtures/app-sidebar";

vi.mock("./changelog-entries", () => {
  throw new Error("Synthetic notes chunk load failure");
});

it("contains a failed notes chunk without disabling navigation or workspace controls", async () => {
  const user = userEvent.setup();
  const props = renderSidebar();
  await user.click(screen.getByRole("button", { name: /Changelog/ }));
  expect(await screen.findByText("Changelog unavailable. Reload the page to retry.")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: /Console/ })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Switch" }));
  expect(props.onSwitchDatabase).toHaveBeenCalledOnce();
});
