import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { PostgresCredentialFormTemplate } from "./credential-form";

type Props = ComponentProps<typeof PostgresCredentialFormTemplate>;
function renderForm(overrides: Partial<Props> = {}) {
  const props: Props = {
    targets: [{ id: 1, name: "Database", connector_kind: "postgres" }],
    form: { target_id: "1", profile_label: "reader", risk_label: "read-only", username: "reader", password: "" },
    state: { state: "idle" },
    onChange: vi.fn(),
    onSubmit: vi.fn((event) => event.preventDefault()),
    ...overrides,
  };
  return { ...render(<PostgresCredentialFormTemplate {...props} />), props };
}

it("locks managed identity and password while allowing public metadata edits", async () => {
  const user = userEvent.setup();
  const { props } = renderForm({
    formMode: "edit",
    form: {
      target_id: "1",
      profile_label: "reader",
      risk_label: "read-only",
      username: "reader",
      password: "",
      managed_by_aipermission: true,
    },
  });
  expect(screen.getByLabelText("Username")).toBeDisabled();
  expect(screen.getByLabelText("New password")).toBeDisabled();
  expect(screen.getByLabelText("Connector target")).toBeDisabled();
  await user.type(screen.getByLabelText("Risk label"), "!");
  expect(props.onChange).toHaveBeenCalledWith({ ...props.form, risk_label: "read-only!" });
});

it("requires a password for creation but preserves existing encrypted secrets on edit", () => {
  const { rerender, props } = renderForm();
  expect(screen.getByLabelText("Password")).toBeRequired();
  rerender(<PostgresCredentialFormTemplate {...props} formMode="edit" />);
  expect(screen.getByLabelText("New password")).not.toBeRequired();
  expect(screen.getByLabelText("New password")).toHaveAttribute("placeholder", "Leave blank to keep current password");
});

it("never offers targets of a different connector kind", () => {
  renderForm({ targets: [{ id: 2, name: "Other database", connector_kind: "other" }] });
  expect(screen.getByRole("button", { name: "Create Postgres credential" })).toBeDisabled();
  expect(screen.queryByRole("option", { name: /Other database/ })).not.toBeInTheDocument();
});
