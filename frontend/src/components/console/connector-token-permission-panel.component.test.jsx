import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

function renderPanel({
  compact = false,
  onToggleCompact = () => {},
  permissions = [],
  loadPermissions,
  replacePermissions,
  target = selectedTarget,
  targetProfiles = profiles,
  unreadMessages = [],
  onOpenMessages = vi.fn(),
  omitOptionalProps = false,
} = {}) {
  const replaceTokenConnectorPermissions = vi.fn(replacePermissions || (async () => []));
  const loadConnectorActions = vi.fn(async () => actions);
  const loadAllConnectorPermissions = vi.fn(loadPermissions || (async () => ({})));
  const renderWithPermissions = (nextPermissions, currentTarget = target, currentProfiles = targetProfiles) => (
    <ConnectorTokenPermissionPanel
      tokens={{ state: "ready", data: [{ id: 5, name: "codex", token: "aip_example" }] }}
      selectedTarget={currentTarget}
      targets={{ state: "ready", data: currentProfiles }}
      {...(omitOptionalProps ? {} : { compact, onToggleCompact, unreadMessages })}
      connectorPermissionState={{
        state: "ready",
        data: { 5: nextPermissions },
        actionsByTargetRef: {
          [connectorActionCacheKey(selectedTarget, 11)]: actions,
          [connectorActionCacheKey(selectedTarget, 12)]: actions,
          ...(currentTarget
            ? Object.fromEntries(currentProfiles.map((profile) => [connectorActionCacheKey(currentTarget, profile.profile_id), actions]))
            : {}),
        },
        error: null,
      }}
      loadAllConnectorPermissions={loadAllConnectorPermissions}
      loadConnectorActions={loadConnectorActions}
      replaceTokenConnectorPermissions={replaceTokenConnectorPermissions}
      onRefresh={async () => {}}
      onOpenMessages={onOpenMessages}
    />
  );
  const view = render(renderWithPermissions(permissions));
  return {
    replaceTokenConnectorPermissions,
    loadConnectorActions,
    rerenderPermissions: (nextPermissions) => view.rerender(renderWithPermissions(nextPermissions)),
    rerenderTarget: (nextTarget, nextProfiles = [nextTarget]) =>
      view.rerender(renderWithPermissions(permissions, nextTarget, nextProfiles)),
    loadAllConnectorPermissions,
  };
}

beforeEach(() => {
  window.localStorage.clear();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (_url, options = {}) =>
        new Response(
          JSON.stringify({
            items: [{ project_id: 3, enabled: options.method !== "PUT" }],
            revision: options.method === "PUT" ? "scope-2" : "scope-1",
          }),
          { status: 200 },
        ),
    ),
  );
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ConnectorTokenPermissionPanel modes", () => {
  it("uses the expanded defaults when optional panel props are omitted", async () => {
    renderPanel({ omitOptionalProps: true });

    expect(await screen.findByText("Tokens")).toBeVisible();
    expect(screen.queryByTitle("Collapse tokens")).not.toBeInTheDocument();
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

  it("keeps the panel inert until a connector target is selected", async () => {
    const { loadConnectorActions } = renderPanel({ target: null });

    expect(screen.getByText("Select a connector")).toBeVisible();
    expect(await screen.findByText("No credential profiles for this connector.")).toBeVisible();
    expect(loadConnectorActions).not.toHaveBeenCalled();
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

  it("applies grouped and advanced rules only to their selected actions", async () => {
    const user = userEvent.setup();
    const { replaceTokenConnectorPermissions } = renderPanel();

    await user.click(await screen.findByRole("button", { name: "Grouped" }));
    await user.click(within(screen.getByRole("group", { name: "Read operations permission" })).getByRole("button", { name: "Prompt" }));

    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledOnce());
    expect(replaceTokenConnectorPermissions.mock.calls[0][1].map((permission) => permission.action_name)).toEqual([
      "get_tables",
      "query_readonly",
    ]);

    await user.click(screen.getByRole("button", { name: "Advanced" }));
    await user.click(within(screen.getByRole("group", { name: "create_user permission" })).getByRole("button", { name: "Blocked" }));

    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledTimes(2));
    expect(replaceTokenConnectorPermissions.mock.calls[1][1]).toEqual([
      expect.objectContaining({ action_name: "create_user", execution_rule: "blocked" }),
    ]);
  });

  it("keeps compact token controls interactive", async () => {
    const user = userEvent.setup();
    const onToggleCompact = vi.fn();
    const { loadConnectorActions } = renderPanel({ compact: true, onToggleCompact });

    await user.click(screen.getByTitle("Expand tokens"));
    expect(onToggleCompact).toHaveBeenCalledOnce();

    await user.click(await screen.findByTitle("codex: 0 connector grants"));
    await user.selectOptions(screen.getByLabelText("Profile"), "12");
    await waitFor(() => expect(loadConnectorActions).toHaveBeenCalledWith(expect.objectContaining({ profile_id: 12 })));
  });

  it("keeps compact token controls inert without a selected connector", async () => {
    renderPanel({ compact: true, target: null });

    expect(screen.getByTitle("Select a connector first")).toBeDisabled();
    expect(screen.queryByText("No credential profiles for this connector.")).not.toBeInTheDocument();
  });

  it("returns focus to the compact token trigger after Escape", async () => {
    const user = userEvent.setup();
    renderPanel({ compact: true });
    const trigger = await screen.findByTitle("codex: 0 connector grants");

    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    await user.click(screen.getByLabelText("Profile"));
    await user.keyboard("{Escape}");

    await waitFor(() => expect(trigger).toHaveFocus());
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  it("closes the compact token popover after an outside pointer press", async () => {
    const user = userEvent.setup();
    renderPanel({ compact: true });
    const trigger = await screen.findByTitle("codex: 0 connector grants");
    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    await user.pointer({ target: document.body, keys: "[MouseLeft]" });
    await waitFor(() => expect(trigger).toHaveAttribute("aria-expanded", "false"));
  });

  it("opens unread connector messages for the selected token profile", async () => {
    const onOpenMessages = vi.fn();
    const liveTarget = {
      ...selectedTarget,
      connector_kind: "ssh",
      target_id: 8,
      profile_id: 13,
      runtime_id: 21,
      target_name: "Support mailbox",
    };
    renderPanel({
      target: liveTarget,
      targetProfiles: [liveTarget],
      unreadMessages: [{ runtime_id: 21, token_id: 5 }],
      onOpenMessages,
    });
    await userEvent.click(await screen.findByRole("button", { name: /codex/ }));
    expect(onOpenMessages).toHaveBeenCalledWith(5);
  });
});

describe("ConnectorTokenPermissionPanel mutations", () => {
  it("updates the token project visibility from the permission panel", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.click(await screen.findByRole("button", { name: "Hide" }));

    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        expect.stringMatching(/\/api\/tokens\/5\/project-scopes$/),
        expect.objectContaining({ method: "PUT", body: JSON.stringify({ enabled_project_ids: [], expected_revision: "scope-1" }) }),
      ),
    );
  });

  it("does not replace project scopes before the initial snapshot is loaded", async () => {
    const user = userEvent.setup();
    const projectScopes = deferred();
    fetch.mockImplementation(async (_url, options = {}) => {
      if (options.method === "PUT") throw new Error("project scope mutation must remain disabled while loading");
      return projectScopes.promise;
    });
    renderPanel();

    const loading = await screen.findByRole("button", { name: "Loading..." });
    expect(loading).toBeDisabled();
    await user.click(loading);
    expect(fetch.mock.calls.some(([, options]) => options?.method === "PUT")).toBe(false);

    projectScopes.resolve(
      new Response(JSON.stringify({ items: [{ project_id: 3, enabled: true }], revision: "scope-1" }), { status: 200 }),
    );
    expect(await screen.findByRole("button", { name: "Hide" })).toBeEnabled();
  });

  it("reports a project-scope load failure without enabling mutations", async () => {
    fetch.mockRejectedValue(new Error("scope service unavailable"));
    renderPanel();

    expect(await screen.findByText("scope service unavailable")).toBeVisible();
    expect(screen.getByRole("button", { name: "Loading..." })).toBeDisabled();
  });

  it("uses a complete refreshed project snapshot for visibility replacement", async () => {
    const user = userEvent.setup();
    const refreshedScopes = deferred();
    let getCalls = 0;
    fetch.mockImplementation(async (_url, options = {}) => {
      if (options.method === "PUT") {
        return new Response(
          JSON.stringify({
            items: [
              { project_id: 3, enabled: false },
              { project_id: 4, enabled: true },
            ],
            revision: "scope-3",
          }),
          {
            status: 200,
          },
        );
      }
      getCalls += 1;
      if (getCalls === 1) {
        return new Response(
          JSON.stringify({
            items: [
              { project_id: 3, enabled: true },
              { project_id: 4, enabled: true },
            ],
            revision: "scope-1",
          }),
          { status: 200 },
        );
      }
      return refreshedScopes.promise;
    });
    renderPanel();
    await screen.findByRole("button", { name: "Hide" });

    await user.click(screen.getByTitle("Refresh connector permissions"));
    expect(await screen.findByRole("button", { name: "Loading..." })).toBeDisabled();
    refreshedScopes.resolve(
      new Response(
        JSON.stringify({
          items: [
            { project_id: 3, enabled: true },
            { project_id: 4, enabled: true },
          ],
          revision: "scope-2",
        }),
        { status: 200 },
      ),
    );
    await user.click(await screen.findByRole("button", { name: "Hide" }));

    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        expect.stringMatching(/\/api\/tokens\/5\/project-scopes$/),
        expect.objectContaining({ method: "PUT", body: JSON.stringify({ enabled_project_ids: [4], expected_revision: "scope-2" }) }),
      ),
    );
  });

  it("restores project controls and reports a failed visibility update", async () => {
    const user = userEvent.setup();
    fetch.mockImplementation(async (_url, options = {}) => {
      if (options.method === "PUT") throw new Error("scope update unavailable");
      return new Response(JSON.stringify({ items: [{ project_id: 3, enabled: true }], revision: "scope-1" }), { status: 200 });
    });
    renderPanel();

    await user.click(await screen.findByRole("button", { name: "Hide" }));

    expect(await screen.findByText("scope update unavailable")).toBeVisible();
    expect(screen.getByRole("button", { name: "Hide" })).toBeEnabled();
  });

  it("applies one temporary lifetime to every enabled action in the profile", async () => {
    const now = new Date("2026-08-11T10:00:00Z").getTime();
    vi.spyOn(Date, "now").mockReturnValue(now);
    const user = userEvent.setup();
    const permissions = actions.map((action) => ({
      target_id: 7,
      profile_id: 11,
      action_name: action.name,
      execution_rule: "approval_required",
      expires_at: "",
    }));
    const { replaceTokenConnectorPermissions } = renderPanel({ permissions });

    await user.click(await screen.findByRole("button", { name: "1h", exact: true }));

    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledOnce());
    expect(replaceTokenConnectorPermissions).toHaveBeenCalledWith(
      5,
      permissions.map((permission) => ({ ...permission, expires_at: "2026-08-11T11:00:00.000Z" })),
    );
  });

  it("keeps blocked actions permanent when a profile lifetime changes", async () => {
    const now = new Date("2026-08-11T10:00:00Z").getTime();
    vi.spyOn(Date, "now").mockReturnValue(now);
    const user = userEvent.setup();
    const permissions = actions.map((action, index) => ({
      target_id: 7,
      profile_id: 11,
      action_name: action.name,
      execution_rule: index === 0 ? "blocked" : "approval_required",
      expires_at: "",
    }));
    const { replaceTokenConnectorPermissions } = renderPanel({ permissions });

    await user.click(await screen.findByRole("button", { name: "1h", exact: true }));

    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledOnce());
    expect(replaceTokenConnectorPermissions.mock.calls[0][1]).toEqual([
      { ...permissions[0], expires_at: "" },
      { ...permissions[1], expires_at: "2026-08-11T11:00:00.000Z" },
      { ...permissions[2], expires_at: "2026-08-11T11:00:00.000Z" },
    ]);
  });

  it("shows permission save failures with context and retries the mutation", async () => {
    const user = userEvent.setup();
    let attempts = 0;
    const { replaceTokenConnectorPermissions } = renderPanel({
      replacePermissions: async () => {
        attempts += 1;
        if (attempts === 1) throw new Error("gateway unavailable");
        return [];
      },
    });

    await user.click(await screen.findByRole("button", { name: "Always" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("codex / Admin");
    expect(alert).toHaveTextContent("get_tables, query_readonly, create_user");
    expect(alert).toHaveTextContent("gateway unavailable");

    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  });

  it("rebuilds a retried mutation from the latest permission snapshot", async () => {
    const user = userEvent.setup();
    const revokedPermission = {
      target_id: 99,
      profile_id: 101,
      action_name: "deploy",
      execution_rule: "always_run",
      expires_at: "",
    };
    let attempts = 0;
    const conflict = Object.assign(new Error("permission revision conflict"), { status: 409 });
    const { loadAllConnectorPermissions, replaceTokenConnectorPermissions } = renderPanel({
      permissions: [revokedPermission],
      loadPermissions: async () => ({ 5: [] }),
      replacePermissions: async () => {
        attempts += 1;
        if (attempts === 1) throw conflict;
        return [];
      },
    });

    await user.click(await screen.findByRole("button", { name: "Always" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("permission revision conflict");
    expect(loadAllConnectorPermissions).toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledTimes(2));
    expect(replaceTokenConnectorPermissions.mock.calls[1][1]).toEqual(
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

describe("ConnectorTokenPermissionPanel mutation ownership", () => {
  it("retries a lifetime update only after refreshing the current permission snapshot", async () => {
    const now = new Date("2026-08-11T10:00:00Z").getTime();
    vi.spyOn(Date, "now").mockReturnValue(now);
    const user = userEvent.setup();
    const permissions = actions.map((action) => ({
      target_id: 7,
      profile_id: 11,
      action_name: action.name,
      execution_rule: "approval_required",
      expires_at: "",
    }));
    let attempts = 0;
    const conflict = Object.assign(new Error("permission revision conflict"), { status: 409 });
    const { loadAllConnectorPermissions, replaceTokenConnectorPermissions } = renderPanel({
      permissions,
      loadPermissions: async () => ({ 5: permissions }),
      replacePermissions: async () => {
        attempts += 1;
        if (attempts === 1) throw conflict;
        return [];
      },
    });

    await user.click(await screen.findByRole("button", { name: "1h", exact: true }));
    expect(await screen.findByRole("alert")).toHaveTextContent("permission revision conflict");
    expect(loadAllConnectorPermissions).toHaveBeenCalledWith(expect.any(Array), { requireCurrent: true });

    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(replaceTokenConnectorPermissions).toHaveBeenCalledTimes(2));
  });

  it("disables conflict retry when the current permission snapshot cannot be refreshed", async () => {
    const user = userEvent.setup();
    let loads = 0;
    const conflict = Object.assign(new Error("permission revision conflict"), { status: 409 });
    const { loadAllConnectorPermissions, replaceTokenConnectorPermissions } = renderPanel({
      loadPermissions: async () => {
        loads += 1;
        if (loads === 1) return { 5: [] };
        throw new Error("permission refresh failed");
      },
      replacePermissions: async () => {
        throw conflict;
      },
    });
    await waitFor(() => expect(loadAllConnectorPermissions).toHaveBeenCalledTimes(1));

    await user.click(await screen.findByRole("button", { name: "Always" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("permission refresh failed");
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    expect(replaceTokenConnectorPermissions).toHaveBeenCalledOnce();
  });
  it("locks profile, mode, and refresh controls while a permission mutation is pending", async () => {
    const user = userEvent.setup();
    const mutation = deferred();
    renderPanel({ replacePermissions: () => mutation.promise });
    await screen.findByRole("button", { name: "Hide" });

    await user.click(screen.getByRole("button", { name: "Always" }));

    expect(screen.getByLabelText("Profile")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Basic" })).toBeDisabled();
    expect(screen.getByTitle("Refresh connector permissions")).toBeDisabled();

    mutation.resolve([]);
    await waitFor(() => expect(screen.getByLabelText("Profile")).toBeEnabled());
  });

  it("does not publish a stale mutation failure after the selected target changes", async () => {
    const user = userEvent.setup();
    const mutation = deferred();
    const { rerenderTarget } = renderPanel({ replacePermissions: () => mutation.promise });
    await user.click(await screen.findByRole("button", { name: "Always" }));

    const nextTarget = { ...selectedTarget, target_id: 8, target_name: "Reporting database", profile_id: 13 };
    rerenderTarget(nextTarget);
    mutation.reject(new Error("old target failed"));

    await waitFor(() => expect(screen.getByLabelText("Profile")).toHaveValue("13"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

  it("does not publish stale success state after the selected target changes", async () => {
    const user = userEvent.setup();
    const mutation = deferred();
    const { rerenderTarget } = renderPanel({ replacePermissions: () => mutation.promise });
    await user.click(await screen.findByRole("button", { name: "Always" }));

    const nextTarget = { ...selectedTarget, target_id: 8, target_name: "Reporting database", profile_id: 13 };
    rerenderTarget(nextTarget);
    mutation.resolve([]);

    await waitFor(() => expect(screen.getByLabelText("Profile")).toHaveValue("13"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("retires conflict recovery when the selected target changes", async () => {
    const user = userEvent.setup();
    const refresh = deferred();
    let loads = 0;
    const conflict = Object.assign(new Error("permission revision conflict"), { status: 409 });
    const { loadAllConnectorPermissions, rerenderTarget } = renderPanel({
      loadPermissions: async () => {
        loads += 1;
        if (loads === 1) return { 5: [] };
        return refresh.promise;
      },
      replacePermissions: async () => {
        throw conflict;
      },
    });
    await waitFor(() => expect(loadAllConnectorPermissions).toHaveBeenCalledOnce());
    await user.click(await screen.findByRole("button", { name: "Always" }));
    await waitFor(() => expect(loadAllConnectorPermissions).toHaveBeenCalledTimes(2));

    const nextTarget = { ...selectedTarget, target_id: 8, target_name: "Reporting database", profile_id: 13 };
    rerenderTarget(nextTarget);
    refresh.resolve({ 5: [] });

    await waitFor(() => expect(screen.getByLabelText("Profile")).toHaveValue("13"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });
});

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}
