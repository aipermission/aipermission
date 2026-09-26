import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { AddCredentialMenu } from "./credentials";

it("supports keyboard navigation and focus return for credential types", async () => {
  const user = userEvent.setup();
  const onAdd = vi.fn();
  render(<AddCredentialMenu kinds={["postgres", "ssh"]} onAdd={onAdd} />);
  const trigger = screen.getByRole("button", { name: "Add credential" });
  trigger.focus();

  await user.keyboard("{ArrowUp}");
  expect(screen.getByRole("menuitem", { name: /SSH/ })).toHaveFocus();
  await user.keyboard("{Home}{Enter}");
  expect(onAdd).toHaveBeenCalledWith("postgres");
  expect(trigger).toHaveFocus();
});
