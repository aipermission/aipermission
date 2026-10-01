import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { RoleReconciliationForm } from "./role-reconciliation-form";
import { roleHistoryFixture } from "../../../../test/postgres/role-history-fixtures.test";
import type { DatabaseProfile } from "../../_shared/database-model-types";

const profiles: DatabaseProfile[] = [
  { id: 2, kind: "username_password", label: "Original admin", public: { username: "admin" } },
  { id: 3, kind: "username_password", label: "Other admin" },
];

function presenceEntry() {
  const entry = roleHistoryFixture();
  entry.record.status = "cleanup_intent";
  return entry;
}

it("requires explicit original-cluster acknowledgement and the recorded admin", () => {
  const entry = presenceEntry();
  const confirm = vi.fn().mockResolvedValue(undefined);
  render(<RoleReconciliationForm entry={entry} profiles={profiles} disabled={false} onConfirm={confirm} />);
  fireEvent.click(screen.getByRole("button", { name: /Verify role presence/ }));
  const submit = screen.getByRole("button", { name: "Confirm role presence" });
  const select = screen.getByRole("combobox", { name: "Reconciliation admin profile" });
  const acknowledgement = screen.getByRole("checkbox");
  expect(select).toHaveValue("2");
  expect(submit).toBeDisabled();
  fireEvent.click(acknowledgement);
  expect(submit).toBeEnabled();
  fireEvent.change(select, { target: { value: "3" } });
  expect(acknowledgement).not.toBeChecked();
  expect(acknowledgement).toBeDisabled();
  expect(submit).toBeDisabled();
  expect(screen.getByText(/does not match recorded admin profile 2/)).toBeInTheDocument();
  fireEvent.change(select, { target: { value: "2" } });
  expect(submit).toBeDisabled();
  fireEvent.click(acknowledgement);
  fireEvent.click(submit);
  expect(confirm).toHaveBeenCalledExactlyOnceWith(entry, 2);
});

it("freezes every decision control while busy and drops consent when reopened", () => {
  const props = { entry: presenceEntry(), profiles, disabled: false, onConfirm: vi.fn().mockResolvedValue(undefined) };
  const { rerender } = render(<RoleReconciliationForm {...props} />);
  const open = screen.getByRole("button", { name: /Verify role presence/ });
  fireEvent.click(open);
  fireEvent.click(screen.getByRole("checkbox"));
  rerender(<RoleReconciliationForm {...props} disabled />);
  for (const control of [
    open,
    screen.getByRole("combobox"),
    screen.getByRole("checkbox"),
    screen.getByRole("button", { name: "Confirm role presence" }),
  ])
    expect(control).toBeDisabled();
  rerender(<RoleReconciliationForm {...props} />);
  fireEvent.click(open);
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  fireEvent.click(open);
  expect(screen.getByRole("checkbox")).not.toBeChecked();
});

it("does not offer profiles of the wrong kind or unsafe identity", () => {
  render(
    <RoleReconciliationForm
      entry={presenceEntry()}
      profiles={[
        { id: 2, kind: "other", label: "Wrong kind" },
        { id: Number.NaN, kind: "username_password", label: "Unsafe identity" },
      ]}
      disabled={false}
      onConfirm={vi.fn()}
    />,
  );
  expect(screen.getByRole("button", { name: /Verify role presence/ })).toBeDisabled();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
});
