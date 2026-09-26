import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { CredentialRow } from "./credential-row";

const row = {
  row_id: "fixture:1:2",
  connector_kind: "fixture",
  connector_label: "Fixture",
  name: "Reader",
  kind: "username_password",
  target_label: "Database",
  target_detail: "database.internal",
  metadata: ["username: reader"],
  profile: { id: 2, label: "Reader" },
};

it("renders connector-owned operations and returns the unchanged native row on edit and delete", async () => {
  const user = userEvent.setup();
  const onEdit = vi.fn<(_row: typeof row) => void>();
  const onDelete = vi.fn<(_row: typeof row) => void>();
  render(
    <table>
      <tbody>
        <CredentialRow
          row={row}
          onEdit={onEdit}
          onDelete={onDelete}
          busy={false}
          operations={<button type="button">Native operation</button>}
        />
      </tbody>
    </table>,
  );
  expect(screen.getByText("database.internal")).toBeVisible();
  expect(screen.getByText("username: reader")).toBeVisible();
  expect(screen.getByRole("button", { name: "Native operation" })).toBeVisible();
  expect(screen.queryByText("None")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  expect(onEdit.mock.calls[0][0]).toBe(row);
  expect(onDelete.mock.calls[0][0].profile).toBe(row.profile);
});

it("keeps unavailable operations and deletion restrictions visible without allowing mutation", async () => {
  const user = userEvent.setup();
  const onEdit = vi.fn();
  const onDelete = vi.fn();
  const restricted = { ...row, target_detail: "", delete_disabled: "Credential is in use" };
  const view = render(
    <table>
      <tbody>
        <CredentialRow row={restricted} onEdit={onEdit} onDelete={onDelete} busy={false} />
      </tbody>
    </table>,
  );
  expect(screen.getByText("None")).toBeVisible();
  expect(screen.queryByText("database.internal")).not.toBeInTheDocument();
  const deleteButton = screen.getByRole("button", { name: "Credential is in use" });
  expect(deleteButton).toBeDisabled();
  await user.click(deleteButton);
  view.rerender(
    <table>
      <tbody>
        <CredentialRow row={row} onEdit={onEdit} onDelete={onDelete} busy />
      </tbody>
    </table>,
  );
  await user.click(screen.getByRole("button", { name: "Edit credential" }));
  await user.click(screen.getByRole("button", { name: "Delete credential" }));
  expect(onEdit).not.toHaveBeenCalled();
  expect(onDelete).not.toHaveBeenCalled();
});
