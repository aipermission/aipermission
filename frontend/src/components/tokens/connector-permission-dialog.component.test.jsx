import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { ConnectorPermissionDialog } from "./connector-permission-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));

const inventory = {
  items: [
    {
      id: 3,
      name: "My Server",
      connector_kind: "ssh",
      profiles: [
        {
          id: 5,
          label: "root",
          actions: [{ name: "exec", description: "Run a command.", risk: "write" }],
        },
      ],
    },
  ],
};

describe("ConnectorPermissionDialog", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPut.mockReset();
    apiPut.mockResolvedValue({ items: [], revision: "permissions-r3" });
  });

  it("does not submit permissions loaded for a previously open token", async () => {
    const user = userEvent.setup();
    const firstPermissions = deferred();
    let firstSignal;
    apiGet.mockImplementation(async (path, options = {}) => {
      if (path === "/api/connectors") return { items: [{ kind: "ssh", label: "SSH" }] };
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
    expect(firstSignal.aborted).toBe(true);
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
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/connectors") return { items: [{ kind: "ssh", label: "SSH" }] };
      if (path === "/api/connector-targets/inventory") return inventory;
      if (path === "/api/tokens/2/connector-permissions") return { items: [], revision: "permissions-r2" };
      throw new Error(`Unexpected GET ${path}`);
    });
    apiPut.mockRejectedValueOnce(new Error("connector permissions changed; reload before saving"));

    render(<ConnectorPermissionDialog token={{ id: 2, name: "second" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    await screen.findByText("My Server");
    await user.click(screen.getByRole("button", { name: "Save connector permissions" }));

    expect(await screen.findByText("connector permissions changed; reload before saving")).toBeInTheDocument();
    expect(screen.queryByText("Connector permissions saved.")).not.toBeInTheDocument();
  });
});

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
