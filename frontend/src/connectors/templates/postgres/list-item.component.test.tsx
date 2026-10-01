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

it("disables credential operations but allows journal inspection without a credential", async () => {
  const user = userEvent.setup();
  const onOperation = vi.fn();
  render(<PostgresConnectorRowActionsTemplate target={{ id: 1 }} profile={null} onOperation={onOperation} />);
  for (const button of [screen.getByTitle("Create managed DB user"), screen.getByTitle("Backup / restore database")]) {
    expect(button).toBeDisabled();
    await user.click(button);
  }
  expect(onOperation).not.toHaveBeenCalled();
  await user.click(screen.getByTitle("Inspect managed role evidence"));
  expect(onOperation).toHaveBeenCalledWith({
    open: true,
    connector_kind: "postgres",
    type: "role-history",
    target: { id: 1 },
    state: "idle",
    error: null,
  });
});

it.each([undefined, 0, -1, Number.MAX_SAFE_INTEGER + 1])("rejects an unavailable journal target %s", (id) => {
  render(<PostgresConnectorRowActionsTemplate target={{ id }} onOperation={vi.fn()} />);
  expect(screen.getByTitle("Inspect managed role evidence")).toBeDisabled();
});
