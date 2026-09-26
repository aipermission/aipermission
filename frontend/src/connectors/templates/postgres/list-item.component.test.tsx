import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { PostgresConnectorRowActionsTemplate } from "./list-item";

it("binds both row operations to the selected target and credential", async () => {
  const user = userEvent.setup();
  const onOperation = vi.fn();
  const target = { id: 1, name: "Test database" };
  const profile = { id: 2, ref: "postgres:1:2" };
  render(<PostgresConnectorRowActionsTemplate target={target} profile={profile} onOperation={onOperation} />);
  await user.click(screen.getByTitle("Create managed DB user"));
  await user.click(screen.getByTitle("Backup / restore database"));
  expect(onOperation).toHaveBeenNthCalledWith(1, {
    open: true,
    connector_kind: "postgres",
    type: "provision-user",
    target,
    profile,
    state: "idle",
    error: null,
  });
  expect(onOperation).toHaveBeenNthCalledWith(2, {
    open: true,
    connector_kind: "postgres",
    type: "backup-restore",
    target,
    profile,
    state: "idle",
    error: null,
  });
});

it("disables operations when no credential is selected", async () => {
  const user = userEvent.setup();
  const onOperation = vi.fn();
  render(<PostgresConnectorRowActionsTemplate target={{ id: 1 }} profile={null} onOperation={onOperation} />);
  for (const button of screen.getAllByRole("button")) {
    expect(button).toBeDisabled();
    await user.click(button);
  }
  expect(onOperation).not.toHaveBeenCalled();
});
