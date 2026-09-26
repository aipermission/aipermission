import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { usePostgresProvisioning } from "./use-postgres-provisioning";
import type { PostgresOperation, ProvisionOperationProps, ProvisionResult } from "./operation-types";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPost).mockImplementation(async (path) =>
    path.endsWith("/provision")
      ? { profile: { id: 9, label: "reader" }, result: { display_text: "Role created." } }
      : completed(metadataOutput("public", "users")),
  );
});

function operation(id = 1): PostgresOperation {
  return {
    open: true,
    connector_kind: "postgres",
    type: "provision-user",
    target: { id, name: `db-${id}`, config: { database: `app_${id}` } },
    profile: { id: id * 10, ref: `postgres:${id}:${id * 10}` },
  };
}

function renderProvisioning(overrides: Partial<ProvisionOperationProps> = {}) {
  const props: ProvisionOperationProps = { value: operation(), onOperationComplete: vi.fn().mockResolvedValue(undefined), ...overrides };
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

  it("keeps pending metadata separate from loaded schema data", async () => {
    vi.mocked(apiPost).mockResolvedValue({ status: "approval_pending", display_text: "Waiting for approval" });
    const { result } = renderProvisioning();

    await waitFor(() => expect(result.current.metadata.state).toBe("pending"));
    expect(result.current.metadata.schemas).toEqual([]);
    expect(result.current.metadata.error).toBe("Metadata request is awaiting approval.");
  });

  it("reports provisioning rejection without refreshing inventory or claiming a profile was created", async () => {
    vi.mocked(apiPost).mockImplementation(async (path) => {
      if (path.endsWith("/provision")) throw new Error("Provisioning permission denied");
      return completed(metadataOutput("public", "users"));
    });
    const { result, props } = renderProvisioning();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => result.current.updateForm("role_name", "reader"));

    await act(async () => result.current.provisionUser({ preventDefault: vi.fn() }));

    expect(result.current.state).toEqual({ state: "error", error: "Provisioning permission denied", result: null });
    expect(props.onOperationComplete).not.toHaveBeenCalled();
  });

  it("discards metadata returned after the target profile changes", async () => {
    const pending = new Map<string, (_response: ReturnType<typeof completed>) => void>();
    vi.mocked(apiPost).mockImplementation((_path, payload) => new Promise<ReturnType<typeof completed>>((resolve) => pending.set(connectorActionRequest(payload).target_ref, resolve)));
    const { result, rerender, props } = renderProvisioning();
    await waitFor(() => expect(pending.has("postgres:1:10")).toBe(true));

    rerender({ ...props, value: operation(2) });
    await waitFor(() => expect(pending.has("postgres:2:20")).toBe(true));
    await act(async () => {
      const resolve = pending.get("postgres:1:10");
      if (!resolve) throw new Error("Old metadata request was not captured");
      resolve(completed(metadataOutput("stale", "ignored")));
    });
    expect(result.current.metadata.schemas).toEqual([]);
    await act(async () => {
      const resolve = pending.get("postgres:2:20");
      if (!resolve) throw new Error("Current metadata request was not captured");
      resolve(completed(metadataOutput("current", "events")));
    });
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

  it("blocks read and change until selected tables explicitly include all columns", async () => {
    const { result } = renderProvisioning();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => {
      result.current.setScope({
        all_schemas: false,
        schemas: {
          public: {
            selected: true,
            all_tables: false,
            tables: { users: { selected: true, all_columns: false, columns: { id: true } } },
          },
        },
      });
      result.current.updateForm("role_name", "app_writer");
    });

    act(() => result.current.updateForm("preset", "read_write"));
    expect(result.current.form.preset).toBe("read_only");
    expect(result.current.state.error).toMatch(/requires all columns/i);

    act(() => {
      result.current.setScope((current) => ({
        ...current,
        schemas: {
          ...current.schemas,
          public: {
            ...current.schemas.public,
            tables: { ...current.schemas.public.tables, users: { ...current.schemas.public.tables.users, all_columns: true } },
          },
        },
      }));
    });
    act(() => result.current.updateForm("preset", "read_write"));

    expect(result.current.form.preset).toBe("read_write");
    expect(result.current.state.error).toBe("");
    const selectedScope = result.current.selectedScope;
    if (!selectedScope || selectedScope.all_schemas) throw new Error("Scoped selection was not preserved");
    const schema = selectedScope.schemas[0];
    if (schema.all_tables) throw new Error("Table selection was not preserved");
    expect(schema.tables[0]).toEqual({ table: "users", all_columns: true });
    expect(result.current.canSubmit).toBe(true);
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
    let resolveProvision: ((_response: ProvisionResult) => void) | undefined;
    vi.mocked(apiPost).mockImplementation((path) => {
      if (path.endsWith("/provision")) return new Promise<ProvisionResult>((resolve) => (resolveProvision = resolve));
      return Promise.resolve(completed(metadataOutput("public", "users")));
    });
    const { result, rerender, props } = renderProvisioning();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => result.current.updateForm("role_name", "app_reader"));
    let provisionPromise: Promise<void> | undefined;
    act(() => {
      provisionPromise = result.current.provisionUser({ preventDefault: vi.fn() });
    });

    rerender({ ...props, value: operation(2) });
    await act(async () => {
      if (!resolveProvision) throw new Error("Provisioning request was not captured");
      resolveProvision({ profile: { id: 9, label: "stale" } });
      await provisionPromise;
    });

    expect(result.current.state).toEqual({ state: "idle", error: "", result: null });
    expect(props.onOperationComplete).not.toHaveBeenCalled();
  });
});

function completed(output: ReturnType<typeof metadataOutput>) {
  return { request_id: 1, status: "completed" as const, output };
}

function metadataOutput(schema: string, table: string) {
  return { rows: [{ table_schema: schema, table_name: table, columns: ["id"] }] };
}
