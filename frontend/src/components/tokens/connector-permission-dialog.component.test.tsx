import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { ConnectorPermissionDialog } from "./connector-permission-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));

const inventory = {
  items: [
    {
      id: 3,
      project_id: 1,
      project_name: "My Project",
      project_slug: "my-project",
      status: "ready",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      name: "My Server",
      connector_kind: "ssh",
      profiles: [
        {
          id: 5,
          target_id: 3,
          connector_kind: "ssh",
          vault_session_supported: true,
          kind: "private_key",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          label: "root",
          actions: [{ name: "exec", description: "Run a command.", risk: "write" }],
        },
      ],
    },
  ],
};

describe("ConnectorPermissionDialog", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPut).mockReset();
    vi.mocked(apiPut).mockResolvedValue({ items: [], revision: "permissions-r3" });
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/connectors") return { items: [{ kind: "ssh", label: "SSH", version: "0.2" }] };
      if (path === "/api/connector-targets/inventory") return inventory;
      if (/^\/api\/tokens\/\d+\/connector-permissions$/.test(path)) return { items: [], revision: "permissions-r2" };
      throw new Error(`Unexpected GET ${path}`);
    });
  });

  it("does not submit permissions loaded for a previously open token", async () => {
    const user = userEvent.setup();
    const firstPermissions = deferred();
    let firstSignal: AbortSignal | undefined;
    vi.mocked(apiGet).mockImplementation(async (path, options = {}) => {
      if (path === "/api/connectors") return { items: [{ kind: "ssh", label: "SSH", version: "0.2" }] };
      if (path === "/api/connector-targets/inventory") return inventory;
      if (path === "/api/tokens/1/connector-permissions") {
        firstSignal = options.signal;
        return firstPermissions.promise;
      }
      if (path === "/api/tokens/2/connector-permissions") return { items: [], revision: "permissions-r2" };
      throw new Error(`Unexpected GET ${path}`);
    });

    const view = render(<ConnectorPermissionDialog token={{ id: 1, name: "first" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    await waitFor(() => expect(firstSignal).toBeDefined());
    view.rerender(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);

    await screen.findByText("My Server");
    expect(firstSignal?.aborted).toBe(true);
    firstPermissions.resolve({
      items: [{ target_id: 3, profile_id: 5, action_name: "exec", execution_rule: "always_run" }],
    });
    await Promise.resolve();

    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));
    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith(
        "/api/tokens/2/connector-permissions",
        { permissions: [], expected_revision: "permissions-r2" },
        { signal: expect.any(AbortSignal) },
      ),
    );
  });

  it("shows a stale revision conflict without reporting a successful save", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut).mockRejectedValueOnce(new Error("connector permissions changed; reload before saving"));

    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    await screen.findByText("My Server");
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));

    expect(await screen.findByText("connector permissions changed; reload before saving")).toBeInTheDocument();
    expect(screen.queryByText("Connector permissions saved.")).not.toBeInTheDocument();
  });

  it("does not report success for a malformed permission save response", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut).mockResolvedValueOnce({ items: [{ action_name: "exec" }], revision: "permissions-r3" });

    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    await screen.findByText("My Server");
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));

    expect(await screen.findByText("Invalid token permission response from gateway.")).toBeInTheDocument();
    expect(screen.queryByText("Connector permissions saved.")).not.toBeInTheDocument();
  });

  it("rejects malformed permission data and keeps saving disabled", async () => {
    vi.mocked(apiGet).mockImplementation(async (path) => {
      if (path === "/api/connectors") return { items: [{ kind: "ssh", label: "SSH", version: "0.2" }] };
      if (path === "/api/connector-targets/inventory") return inventory;
      if (path === "/api/tokens/2/connector-permissions") return { items: "invalid", revision: "permissions-r2" };
      throw new Error(`Unexpected GET ${path}`);
    });

    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    expect(await screen.findByText("Invalid token permissions response from gateway.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save connector permissions" })).toBeDisabled();
    expect(apiPut).not.toHaveBeenCalled();
  });

  it("selects a profile, grants Prompt, and saves only its action", async () => {
    const user = userEvent.setup();
    const onSaved = vi.fn();

    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={onSaved} />);
    await user.click(await screen.findByRole("button", { name: /My Server/ }));
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    expect(screen.getByText("1 connector action grant selected.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));

    await waitFor(() =>
      expect(apiPut).toHaveBeenCalledWith(
        "/api/tokens/2/connector-permissions",
        {
          permissions: [{ target_id: 3, profile_id: 5, action_name: "exec", execution_rule: "approval_required" }],
          expected_revision: "permissions-r2",
        },
        { signal: expect.any(AbortSignal) },
      ),
    );
    expect(onSaved).toHaveBeenCalledOnce();
  });

  it("freezes the submitted draft and dismissal through response and refresh, then clears saved on edit", async () => {
    const user = userEvent.setup();
    const response = deferred();
    const refresh = deferred();
    const onSaved = vi.fn(() => refresh.promise as Promise<void>);
    const onClose = vi.fn();
    vi.mocked(apiPut).mockReturnValueOnce(response.promise);
    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={onClose} onSaved={onSaved} />);
    await user.click(await screen.findByRole("button", { name: /My Server/ }));
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));
    for (const name of ["Disabled", "Blocked", "Prompt", "Always", "Close", "Close dialog"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
      await user.click(screen.getByRole("button", { name }));
    }
    await user.keyboard("{Escape}");
    fireEvent.pointerDown(screen.getByTestId("dialog-overlay"));
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByText("1 connector action grant selected.")).toBeVisible();
    await act(async () => response.resolve(savedAlways));
    expect(onSaved).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "Disabled" })).toBeDisabled();
    expect(screen.queryByText("Connector permissions saved.")).not.toBeInTheDocument();
    await act(async () => refresh.resolve(undefined));
    expect(await screen.findByText("Connector permissions saved.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Disabled" }));
    expect(screen.queryByText("Connector permissions saved.")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));
    expect(apiPut).toHaveBeenLastCalledWith(
      "/api/tokens/2/connector-permissions",
      { permissions: [], expected_revision: "permissions-r3" },
      { signal: expect.any(AbortSignal) },
    );
  });

  it("admits only one synchronous submit before a pending mutation paints", async () => {
    const response = deferred();
    vi.mocked(apiPut).mockReturnValueOnce(response.promise);
    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} />);
    await screen.findByText("My Server");
    const form = screen.getByRole("button", { name: "Save connector permissions" }).closest("form")!;
    act(() => {
      fireEvent.submit(form);
      fireEvent.submit(form);
    });
    expect(apiPut).toHaveBeenCalledOnce();
    await act(async () => response.resolve({ items: [], revision: "permissions-r3" }));
  });

  it("ignores a replaced token's late save even after returning to the same token", async () => {
    const user = userEvent.setup();
    const response = deferred();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockReturnValueOnce(response.promise);
    const view = render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("My Server");
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));
    const signal = vi.mocked(apiPut).mock.calls[0][2]?.signal;
    view.rerender(<ConnectorPermissionDialog token={{ id: 1, name: "first" }} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("My Server");
    view.rerender(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("My Server");
    expect(signal?.aborted).toBe(true);
    await act(async () => response.resolve(savedAlways));
    expect(onSaved).not.toHaveBeenCalled();
    expect(screen.queryByText("Connector permissions saved.")).not.toBeInTheDocument();
    expect(screen.getByText("0 connector action grants selected.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save connector permissions" })).toBeEnabled();
  });

  it("distinguishes a committed permission save from a failed parent refresh", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut).mockResolvedValueOnce(savedAlways);
    render(
      <ConnectorPermissionDialog
        token={{ id: 2, name: "second" }}
        onClose={vi.fn()}
        onSaved={async () => {
          throw new Error("refresh unavailable");
        }}
      />,
    );
    await user.click(await screen.findByRole("button", { name: /My Server/ }));
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));
    expect(await screen.findByText(/saved, but refreshing token data failed: refresh unavailable/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Disabled" })).toBeEnabled();
  });
});

const savedAlways = {
  items: [
    {
      project_id: 1,
      project_name: "My Project",
      project_slug: "my-project",
      project_enabled: true,
      target_id: 3,
      target_name: "My Server",
      profile_id: 5,
      profile_label: "root",
      target_ref: "ssh:3:5",
      connector_kind: "ssh",
      profile_kind: "private_key",
      action_name: "exec",
      execution_rule: "always_run",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  revision: "permissions-r3",
};

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise<unknown>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
