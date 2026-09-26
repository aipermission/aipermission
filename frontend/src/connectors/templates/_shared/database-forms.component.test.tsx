import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ClickHouseConnectorFormTemplate } from "../clickhouse/form";
import { PostgresConnectorFormTemplate } from "../postgres/form";
import type { DatabaseConnectionForm } from "./database-form-types";

const form: DatabaseConnectionForm & { ssl_mode: string; tls_mode: string } = {
  name: "My database",
  connection_mode: "direct",
  transport_target_ref: "",
  host: "127.0.0.1",
  port: 5432,
  database: "app",
  ssl_mode: "auto",
  tls_mode: "auto",
  profile_label: "readonly",
  risk_label: "read-only",
  username: "reader",
  password: "",
};

it.each([
  { name: "Postgres", Template: PostgresConnectorFormTemplate, passwordRequired: true },
  { name: "ClickHouse", Template: ClickHouseConnectorFormTemplate, passwordRequired: false },
])("keeps $name creation and edit password requirements distinct", ({ Template, passwordRequired }) => {
  const props = { form, onChange: vi.fn() };
  const { rerender } = render(<Template {...props} />);
  expect(screen.getByLabelText("Password").hasAttribute("required")).toBe(passwordRequired);

  rerender(<Template {...props} mode="edit" />);
  expect(screen.getByLabelText("Password")).not.toBeRequired();
  expect(screen.getByLabelText("Password")).toHaveAttribute("placeholder", "Leave blank to keep the current encrypted password");
});

it.each([
  { name: "Postgres", Template: PostgresConnectorFormTemplate, modeLabel: "SSL mode", field: "ssl_mode" },
  { name: "ClickHouse", Template: ClickHouseConnectorFormTemplate, modeLabel: "TLS mode", field: "tls_mode" },
])("routes $name transport and TLS edits through the supplied form callback", async ({ Template, modeLabel, field }) => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(<Template form={form} onChange={onChange} />);

  await user.selectOptions(screen.getByLabelText(modeLabel), "verify_full");
  expect(onChange).toHaveBeenLastCalledWith(field, "verify_full");
  await user.selectOptions(screen.getByLabelText("Connection mode"), "over_ssh");
  expect(onChange).toHaveBeenLastCalledWith("connection_mode", "over_ssh");
});
