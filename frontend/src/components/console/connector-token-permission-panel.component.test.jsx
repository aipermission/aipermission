import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { connectorActionCacheKey } from "../../lib/use-connector-permissions";
import { ConnectorTokenPermissionPanel } from "./connector-token-permission-panel";

const selectedTarget = {
  connector_kind: "postgres",
  target_id: 7,
  target_name: "Application database",
  profile_id: 11,
  profile_label: "Admin",
  project_id: 3,
  project_name: "My Project",
};
const profiles = [selectedTarget, { ...selectedTarget, profile_id: 12, profile_label: "Read only" }];
const actions = [
  { name: "get_tables", description: "List tables", risk: "read", category: "schema" },
  { name: "query_readonly", description: "Run a read query", risk: "read", category: "query" },
  { name: "create_user", description: "Create a user", risk: "write", category: "users" },
];

function renderPanel({ permissions = [] } = {}) {
  const replaceTokenConnectorPermissions = vi.fn(async () => []);
  const loadConnectorActions = vi.fn(async () => actions);
  const loadAllConnectorPermissions = vi.fn(async () => ({}));
  render(
    <ConnectorTokenPermissionPanel
      tokens={{ state: "ready", data: [{ id: 5, name: "codex", token: "aip_example" }] }}
      selectedTarget={selectedTarget}
      targets={{ state: "ready", data: profiles }}
      connectorPermissionState={{
        state: "ready",
        data: { 5: permissions },
        actionsByTargetRef: {
          [connectorActionCacheKey(selectedTarget, 11)]: actions,
          [connectorActionCacheKey(selectedTarget, 12)]: actions,
        },
        error: null,
      }}
      loadAllConnectorPermissions={loadAllConnectorPermissions}
      loadConnectorActions={loadConnectorActions}
      replaceTokenConnectorPermissions={replaceTokenConnectorPermissions}
      onToggleCompact={() => {}}
      onRefresh={async () => {}}
    />,
  );
  return { replaceTokenConnectorPermissions, loadConnectorActions };
}

describe("ConnectorTokenPermissionPanel", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ items: [{ project_id: 3, enabled: true }] }), { status: 200 })),
    );
  });

  it("selects and persists a connector credential profile", async () => {
    const user = userEvent.setup();
    const { loadConnectorActions } = renderPanel();
    const profile = await screen.findByLabelText("Profile");

    expect(profile).toHaveValue("11");
    await user.selectOptions(profile, "12");

    expect(profile).toHaveValue("12");
    expect(window.localStorage.getItem("aipermission.console.profile:postgres:7:5")).toBe("12");
    await waitFor(() => expect(loadConnectorActions).toHaveBeenCalledWith(expect.objectContaining({ profile_id: 12 })));
  });

  it("infers grouped permissions and lets the user switch to advanced controls", async () => {
    const user = userEvent.setup();
    renderPanel({
      permissions: actions.map((action) => ({
        target_id: 7,
        profile_id: 11,
        action_name: action.name,
        execution_rule: action.risk === "read" ? "always_run" : "approval_required",
      })),
    });

    expect(await screen.findByRole("button", { name: "Grouped" })).toHaveClass("permission-button-active");
    expect(screen.getByText("Read operations")).toBeInTheDocument();
    expect(screen.getByText("Write operations")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Advanced" }));
    expect(screen.getByText("get_tables")).toBeInTheDocument();
    expect(screen.getByText("create_user")).toBeInTheDocument();
  });

  it("applies a Basic preset to every action in the selected profile", async () => {
    const user = userEvent.setup();
    const { replaceTokenConnectorPermissions } = renderPanel();

    expect(await screen.findByRole("button", { name: "Basic" })).toHaveClass("permission-button-active");
    await user.click(screen.getByRole("button", { name: "Always" }));

    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledOnce());
    expect(replaceTokenConnectorPermissions).toHaveBeenCalledWith(
      5,
      actions.map((action) => ({
        target_id: 7,
        profile_id: 11,
        action_name: action.name,
        execution_rule: "always_run",
        expires_at: "",
      })),
    );
  });
});
