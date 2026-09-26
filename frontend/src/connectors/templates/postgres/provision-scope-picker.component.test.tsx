import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { ProvisionScopePicker } from "./provision-scope-picker";
import type { ProvisionForm, ScopeSelection } from "./provisioning-types";

function Picker({ preset = "read_only" }: { preset?: ProvisionForm["preset"] }) {
  const [scope, setScope] = useState<ScopeSelection>({ all_schemas: false, schemas: {} });
  return (
    <ProvisionScopePicker
      metadata={{ state: "ready", schemas: [{ name: "public", tables: [{ name: "users", columns: ["id", "email"] }] }] }}
      scope={scope}
      onChange={setScope}
      preset={preset}
    />
  );
}

it("narrows schema, table and column selections using functional state updates", async () => {
  const user = userEvent.setup();
  render(<Picker />);
  await user.click(screen.getByRole("checkbox", { name: "Select schema public" }));
  await user.click(screen.getByRole("checkbox", { name: "All tables and columns in this schema" }));
  await user.click(screen.getByRole("checkbox", { name: "Select table users" }));
  await user.click(screen.getByRole("checkbox", { name: "All columns" }));
  await user.click(screen.getByRole("checkbox", { name: "email" }));
  expect(screen.getByRole("checkbox", { name: "email" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "id" })).not.toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "Select schema public" }));
  expect(screen.queryByRole("checkbox", { name: "Select table users" })).not.toBeInTheDocument();
});

it("requires all columns for the write preset", async () => {
  const user = userEvent.setup();
  render(<Picker preset="read_write" />);
  await user.click(screen.getByRole("checkbox", { name: "Select schema public" }));
  await user.click(screen.getByRole("checkbox", { name: "All tables and columns in this schema" }));
  await user.click(screen.getByRole("checkbox", { name: "Select table users" }));
  const allColumns = screen.getByRole("checkbox", { name: "All columns required for read and change preset" });
  expect(allColumns).toBeDisabled();
  expect(allColumns).toBeChecked();
  expect(screen.queryByRole("checkbox", { name: "email" })).not.toBeInTheDocument();
});

it("distinguishes loading metadata from an empty scope", () => {
  const scope: ScopeSelection = { all_schemas: false, schemas: {} };
  const { rerender } = render(
    <ProvisionScopePicker metadata={{ state: "loading", schemas: [] }} scope={scope} onChange={() => {}} preset="read_only" />,
  );
  expect(screen.getByText("Loading schema metadata...")).toBeVisible();
  rerender(<ProvisionScopePicker metadata={{ state: "ready", schemas: [] }} scope={scope} onChange={() => {}} preset="read_only" />);
  expect(screen.getByText(/No schema metadata loaded/)).toBeVisible();
});
