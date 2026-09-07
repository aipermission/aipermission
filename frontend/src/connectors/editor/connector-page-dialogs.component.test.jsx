import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { AddConnectorMenu, ConnectorEditorDrawer } from "./connector-page-dialogs";

it("lists only connector kinds shared by the backend catalog and frontend templates", async () => {
  const user = userEvent.setup();
  const onAdd = vi.fn();
  render(
    <AddConnectorMenu
      catalog={{ state: "ready", data: [{ kind: "ssh" }, { kind: "backend-only" }], details: { ssh: { label: "SSH", version: "0.2" } } }}
      onAdd={onAdd}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Add connector" }));
  await user.click(screen.getByRole("menuitem", { name: /SSH/ }));
  expect(onAdd).toHaveBeenCalledWith("ssh");
  expect(screen.queryByText("backend-only")).not.toBeInTheDocument();
});

it("owns keyboard navigation, Escape dismissal, and focus return", async () => {
  const user = userEvent.setup();
  const onAdd = vi.fn();
  render(
    <AddConnectorMenu
      catalog={{
        state: "ready",
        data: [{ kind: "ssh" }, { kind: "postgres" }],
        details: { ssh: { label: "SSH" }, postgres: { label: "Postgres" } },
      }}
      onAdd={onAdd}
    />,
  );
  const trigger = screen.getByRole("button", { name: "Add connector" });
  trigger.focus();
  await user.keyboard("{ArrowDown}");
  expect(screen.getByRole("menuitem", { name: /Postgres/ })).toHaveFocus();
  await user.keyboard("{ArrowDown}");
  expect(screen.getByRole("menuitem", { name: /SSH/ })).toHaveFocus();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();

  await user.keyboard("{ArrowUp}");
  expect(screen.getByRole("menuitem", { name: /SSH/ })).toHaveFocus();
});

it("keeps connector forms scoped to the selected project", async () => {
  const user = userEvent.setup();
  const editor = { closeEditor: vi.fn(), save: vi.fn((event) => event.preventDefault()), selectKind: vi.fn(), updateField: vi.fn() };
  function FormTemplate({ targets }) {
    return <p>{`Visible targets: ${targets.map((target) => target.name).join(",")}`}</p>;
  }
  render(
    <ConnectorEditorDrawer
      drawer={{ open: true, mode: "create", target: null }}
      form={{ connector_kind: "ssh", project_id: "2" }}
      state={{ state: "idle", error: null }}
      connectorOptions={[{ kind: "ssh", label: "SSH" }]}
      projects={[
        { id: 1, name: "One" },
        { id: 2, name: "Two" },
      ]}
      credentials={[]}
      targets={[
        { id: 1, project_id: 1, name: "Hidden" },
        { id: 2, project_id: 2, name: "Visible" },
      ]}
      activeConnectorModel={{ submitLabel: () => "Create connector" }}
      activeCredential={null}
      FormTemplate={FormTemplate}
      editor={editor}
    />,
  );
  expect(screen.getByText("Visible targets: Visible")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Create connector" }));
  expect(editor.save).toHaveBeenCalledOnce();
});
