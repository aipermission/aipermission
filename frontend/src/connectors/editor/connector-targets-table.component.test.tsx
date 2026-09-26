import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConnectorTargetsTable } from "./connector-targets-table";
import type { ConnectorTargetsTableProps, TargetRowActionsProps, TargetTableTemplate } from "./connector-target-table-types";
import { inventoryProfileFixture, inventoryTargetFixture, gatewayTargetFixture } from "../../test/connector-inventory-fixtures";

function tableProps(): ConnectorTargetsTableProps {
  const target = inventoryTargetFixture({
    connector_kind: "example", name: "Fixture target",
    profiles: [
      inventoryProfileFixture({ connector_kind: "example", ref: "example:3:11" }),
      inventoryProfileFixture({ id: 22, connector_kind: "example", ref: "example:3:22", label: "Other" }),
    ],
  });
  return {
    targets: { state: "ready", data: [target], error: null },
    projects: [{ id: 7, name: "My Project", slug: "my-project", target_count: 1 }],
    search: "", collapsedProjects: {}, onSearch: vi.fn(), onToggleProject: vi.fn(),
    catalog: { state: "ready", data: [{ kind: "example", label: "Example", version: "1" }], details: {}, detailFailures: [], error: null },
    unifiedTargets: [], credentials: [], profileSelections: {}, tests: {},
    onSelectProfile: vi.fn(), onTestConnector: vi.fn(), onOperation: vi.fn(),
    onUnderConstruction: vi.fn(), onEdit: vi.fn(), onDelete: vi.fn(),
    resolveTemplate: vi.fn(() => ({ model: { targetEndpoint: () => "fixture-endpoint", credentialHint: () => "Fixture credential" }, RowActions: null })),
  };
}

describe("ConnectorTargetsTable", () => {
  it("binds shared actions to the selected connector and profile", () => {
    const props = tableProps();
    render(<ConnectorTargetsTable {...props} />);

    expect(screen.getByText("fixture-endpoint")).toBeVisible();
    expect(screen.getByText("Fixture credential")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
    fireEvent.click(screen.getByRole("button", { name: "Edit connector" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete connector" }));
    const target = props.targets.data[0];
    expect(props.onTestConnector).toHaveBeenCalledWith(target, target.profiles?.[0]);
    expect(props.onEdit).toHaveBeenCalledWith(target, target.profiles?.[0]);
    expect(props.onDelete).toHaveBeenCalledWith(target);
  });

  it("passes connector-owned operation slots their bound target and profile", () => {
    const props = tableProps();
    props.resolveTemplate = () => ({
      model: {},
      RowActions: ({ target, profile, onOperation, onUnderConstruction }: TargetRowActionsProps) => (
        <div>
          <span>{target.name} / {profile?.label}</span>
          <button onClick={() => onOperation({ open: true, connector_kind: target.connector_kind })}>Fixture operation</button>
          <button onClick={() => onUnderConstruction("Fixture future action")}>Fixture future</button>
        </div>
      ),
    });
    render(<ConnectorTargetsTable {...props} />);

    fireEvent.click(screen.getByText("Fixture operation"));
    fireEvent.click(screen.getByText("Fixture future"));
    expect(props.onOperation).toHaveBeenCalledWith({ open: true, connector_kind: "example" });
    expect(props.onUnderConstruction).toHaveBeenCalledWith("Fixture future action");
    expect(screen.getByText("Fixture target / Default")).toBeVisible();
  });

  it("retains profile selection and resolves its corresponding runtime", async () => {
    const user = userEvent.setup();
    const props = tableProps();
    props.profileSelections = { "example:3": "22" };
    const runtime = gatewayTargetFixture({ profile_id: 22, ref: "example:3:22" });
    props.unifiedTargets = [runtime];
    const endpoint = vi.fn<NonNullable<TargetTableTemplate["model"]["targetEndpoint"]>>(() => "selected-endpoint");
    props.resolveTemplate = () => ({ model: { targetEndpoint: endpoint }, RowActions: null });
    render(<ConnectorTargetsTable {...props} />);

    expect(screen.getByRole("combobox")).toHaveValue("22");
    expect(endpoint).toHaveBeenCalledWith({ target: props.targets.data[0], profile: props.targets.data[0].profiles?.[1], runtime });
    await user.selectOptions(screen.getByRole("combobox"), "11");
    expect(props.onSelectProfile).toHaveBeenCalledWith(props.targets.data[0], "11");
  });

  it("respects connector-owned edit and delete eligibility", () => {
    const props = tableProps();
    props.resolveTemplate = () => ({ model: { canEdit: () => false, canDelete: () => false } });
    render(<ConnectorTargetsTable {...props} />);

    expect(screen.getByRole("button", { name: "Edit connector" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete connector" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled();
  });

  it("requires a profile before testing or editing and reports absent operation slots", () => {
    const props = tableProps();
    props.targets.data[0].profiles = [];
    props.resolveTemplate = () => null;
    render(<ConnectorTargetsTable {...props} />);

    expect(screen.getByText("No profiles")).toBeVisible();
    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Edit connector" })).toBeDisabled();
    expect(screen.getByText(/template not found: example\/row-actions/)).toBeVisible();
  });

  it("renders bounded connection-test feedback without trusting arbitrary result fields", () => {
    const props = tableProps();
    props.tests = { "example:3:11": { state: "testing", error: null, data: null } };
    const view = render(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("Testing...")).toBeVisible();
    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();

    props.tests = { "example:3:11": { state: "ok", error: null, data: { duration_ms: 12 } } };
    view.rerender(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("12ms")).toBeVisible();
    props.tests = { "example:3:11": { state: "ok", error: null, data: { durationMS: 8 } } };
    view.rerender(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("8ms")).toBeVisible();

    props.tests = { "example:3:11": { state: "error", error: null, data: { message: { unexpected: true } } } };
    view.rerender(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("Connection test failed")).toBeVisible();
    props.tests = { "example:3:11": { state: "error", error: "Fixture connection failed", data: null } };
    view.rerender(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("Fixture connection failed")).toBeVisible();
  });

  it("keeps search and project-collapse controls bound to their owners", () => {
    const props = tableProps();
    props.collapsedProjects = { "7": true };
    render(<ConnectorTargetsTable {...props} />);

    expect(screen.queryByText("Fixture target")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /My Project/ }));
    expect(props.onToggleProject).toHaveBeenCalledWith(7);
    fireEvent.change(screen.getByPlaceholderText("Search connectors"), { target: { value: "Fixture" } });
    expect(props.onSearch).toHaveBeenCalledWith("Fixture");
  });

  it("distinguishes loading, empty inventory, and an empty search result", () => {
    const props = tableProps();
    props.targets = { state: "loading", data: [], error: null };
    const view = render(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("Loading connectors...")).toBeVisible();
    props.targets = { state: "ready", data: [], error: null };
    view.rerender(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText(/Create your first connector target/)).toBeVisible();
    props.targets = tableProps().targets;
    props.search = "no-match";
    view.rerender(<ConnectorTargetsTable {...props} />);
    expect(screen.getByText("No connectors match that search.")).toBeVisible();
  });
});
