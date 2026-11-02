import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DatabaseAccountFields } from "../../connectors/templates/_shared/database-account-fields";
import { captureConnectorFamily } from "../../connectors/editor/capture-connector-family";
import { sqlServerConnectorFamily } from "../../connectors/templates/sqlserver/connector-family";
import { SQLServerConnectorFormTemplate } from "../../connectors/templates/sqlserver/form";
import { SQLServerCredentialFormTemplate } from "../../connectors/templates/sqlserver/credential-form";
import * as model from "../../connectors/templates/sqlserver/model";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { renderConnectorFamily } from "../../test/connector-family-host";

it("preserves optional edit passwords while requiring a password for a new account", async () => {
  const onChange = vi.fn();
  const form = { username: "reader", password: "" };
  const view = render(<DatabaseAccountFields form={form} editing onChange={onChange} />);
  expect(screen.getByLabelText("New password")).not.toBeRequired();
  expect(screen.getByLabelText("New password")).toHaveAttribute("placeholder", "Leave blank to keep the current password");
  view.rerender(<DatabaseAccountFields form={form} editing={false} onChange={onChange} />);
  expect(screen.getByLabelText("Password")).toBeRequired();
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Password"), "x");
  expect(onChange).toHaveBeenCalledWith("password", "x");
});

it("defaults the connector form to creating an account with verified TLS", () => {
  render(<SQLServerConnectorFormTemplate form={model.emptyForm()} onChange={vi.fn()} />);
  expect(screen.getByLabelText("Password")).toBeRequired();
  expect(screen.getByLabelText("TLS mode")).toHaveValue("verify_full");
});

it("keeps empty-inventory credential creation disabled and displays its validation error", () => {
  const form = model.emptyCredentialState({ targets: [] }).form;
  render(
    <SQLServerCredentialFormTemplate
      targets={[]}
      form={form}
      state={{ state: "error", error: "Fixture validation error" }}
      onChange={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );
  expect(screen.getByText("Fixture validation error")).toBeVisible();
  expect(screen.getByText(/Create a SQL Server connector before/)).toBeVisible();
  expect(screen.getByRole("button", { name: "Create SQL Server credential" })).toBeDisabled();
  expect(screen.getByLabelText("Password")).toBeRequired();
});

it("keeps credential submission disabled while saving", () => {
  const target = inventoryTargetFixture({ connector_kind: "sqlserver", config: { database: "App" }, profiles: [] });
  const form = model.emptyCredentialState({ targets: [target] }).form;
  render(
    <SQLServerCredentialFormTemplate
      targets={[target]}
      form={form}
      state={{ state: "saving", error: null }}
      onChange={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );
  expect(screen.getByRole("button", { name: "Saving..." })).toBeDisabled();
});

it.each(["no profiles", "missing public identity"])("requires a submitted password when editing inventory with %s", (scenario) => {
  const profile = inventoryProfileFixture({ id: 17, connector_kind: "sqlserver", kind: "username_password", public: undefined });
  const target = inventoryTargetFixture({
    connector_kind: "sqlserver",
    config: { database: "App" },
    profiles: scenario === "no profiles" ? undefined : [profile],
  });
  const family = sqlServerConnectorFamily.create(captureConnectorFamily);
  const host = renderConnectorFamily(family, { targets: [target] });
  act(() => host.commands().openEdit(target, scenario === "no profiles" ? null : profile));
  expect(screen.getByLabelText("New password")).toBeRequired();
  expect(screen.getByLabelText("New password")).toHaveAttribute("placeholder", "Required when changing the username");
});
