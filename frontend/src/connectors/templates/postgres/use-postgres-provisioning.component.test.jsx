import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { usePostgresProvisioning } from "./use-postgres-provisioning";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (path) =>
    path.endsWith("/provision")
      ? { profile: { id: 9, label: "reader" }, result: { display_text: "Role created." } }
      : completed(metadataOutput("public", "users")),
  );
});

function operation(id = 1) {
  return {
    open: true,
    connector_kind: "postgres",
    type: "provision-user",
    target: { id, name: `db-${id}`, config: { database: `app_${id}` } },
    profile: { id: id * 10, ref: `postgres:${id}:${id * 10}` },
  };
}

function renderProvisioning(overrides = {}) {
  const props = { value: operation(), onOperationComplete: vi.fn().mockResolvedValue(undefined), ...overrides };
  return { ...renderHook((next) => usePostgresProvisioning(next), { initialProps: props }), props };
}

describe("usePostgresProvisioning", () => {
  it("loads schema metadata through the selected admin profile", async () => {
    const { result } = renderProvisioning();

    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));

    expect(result.current.metadata.schemas[0]).toEqual({ name: "public", tables: [{ name: "users", columns: ["id"] }] });
    expect(apiPost).toHaveBeenCalledWith(
      "/api/connector-actions/local-run",
      expect.objectContaining({ target_ref: "postgres:1:10", action_name: "query_readonly" }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("discards metadata returned after the target profile changes", async () => {
    const pending = new Map();
    apiPost.mockImplementation((_path, payload) => new Promise((resolve) => pending.set(payload.target_ref, resolve)));
    const { result, rerender, props } = renderProvisioning();
    await waitFor(() => expect(pending.has("postgres:1:10")).toBe(true));

    rerender({ ...props, value: operation(2) });
    await waitFor(() => expect(pending.has("postgres:2:20")).toBe(true));
    await act(async () => pending.get("postgres:1:10")(completed(metadataOutput("stale", "ignored"))));
    expect(result.current.metadata.schemas).toEqual([]);
    await act(async () => pending.get("postgres:2:20")(completed(metadataOutput("current", "events"))));
    await waitFor(() => expect(result.current.metadata.schemas[0]?.name).toBe("current"));
  });

  it("submits a scoped managed credential to the captured target and profile", async () => {
    const onOperationComplete = vi.fn().mockResolvedValue(undefined);
    const { result } = renderProvisioning({ onOperationComplete });
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => result.current.updateForm("role_name", "app_reader"));

    await act(async () => result.current.provisionUser({ preventDefault: vi.fn() }));

    expect(apiPost).toHaveBeenLastCalledWith(
      "/api/connector-targets/1/profiles/10/provision",
      { input: { role_name: "app_reader", profile_label: "app_reader", preset: "read_only", scope: { all_schemas: true } } },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(result.current.state.state).toBe("ready");
    expect(onOperationComplete).toHaveBeenCalledWith({ message: "Managed Postgres credential created." }, operation());
  });

  it("does not report a created credential as failed when inventory refresh fails", async () => {
    const { result } = renderProvisioning({ onOperationComplete: vi.fn().mockRejectedValue(null) });
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => result.current.updateForm("role_name", "app_reader"));

    await act(async () => result.current.provisionUser({ preventDefault: vi.fn() }));

    expect(result.current.state).toMatchObject({
      state: "ready",
      error: "Credential created, but connector refresh failed: unknown error",
      result: { profile: { label: "reader" } },
    });
  });

  it("does not apply a provisioning completion after the target profile changes", async () => {
    let resolveProvision;
    apiPost.mockImplementation((path) => {
      if (path.endsWith("/provision")) return new Promise((resolve) => (resolveProvision = resolve));
      return Promise.resolve(completed(metadataOutput("public", "users")));
    });
    const { result, rerender, props } = renderProvisioning();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => result.current.updateForm("role_name", "app_reader"));
    let provisionPromise;
    act(() => {
      provisionPromise = result.current.provisionUser({ preventDefault: vi.fn() });
    });

    rerender({ ...props, value: operation(2) });
    await act(async () => {
      resolveProvision({ profile: { id: 9, label: "stale" } });
      await provisionPromise;
    });

    expect(result.current.state).toEqual({ state: "idle", error: "", result: null });
    expect(props.onOperationComplete).not.toHaveBeenCalled();
  });
});

function completed(output) {
  return { request_id: 1, status: "completed", output };
}

function metadataOutput(schema, table) {
  return { rows: [{ table_schema: schema, table_name: table, columns: ["id"] }] };
}
