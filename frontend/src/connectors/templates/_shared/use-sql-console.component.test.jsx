import { StrictMode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useSQLConsole } from "./use-sql-console";

// async-owner: src/connectors/templates/_shared/use-sql-metadata.js

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

const config = {
  label: "Test SQL",
  queryAction: "query_readonly",
  describeAction: "describe_table",
  metadataSQL: "SELECT metadata",
  metadataReason: "load test metadata",
  manualReason: "manual test query",
  browserLabel: "Schema",
};

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, payload) =>
    completed(payload.input.sql === config.metadataSQL ? metadataOutput("public", "users") : { columns: ["id"], rows: [{ id: 1 }] }),
  );
});

function renderConsole(overrides = {}, options = {}) {
  const props = {
    config,
    target: { ref: "test-sql:1:1", config: { host: "db", port: 1234, database: "app" } },
    approvals: { data: [] },
    session: { active: true, startedAt: "2026-09-07T12:00:00Z" },
    onRefreshActivity: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  return { ...renderHook((next) => useSQLConsole(next), { initialProps: props, ...options }), props };
}

describe("useSQLConsole", () => {
  it("loads autocomplete metadata through the configured connector action", async () => {
    const { result } = renderConsole();

    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));

    expect(result.current.browserTables).toEqual([expect.objectContaining({ schema: "public", table: "users", columnCount: 1 })]);
    expect(apiPost).toHaveBeenCalledWith(
      "/api/connector-actions/local-run",
      {
        target_ref: "test-sql:1:1",
        action_name: "query_readonly",
        input: { sql: "SELECT metadata", max_rows: 5000 },
        reason: "load test metadata",
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("survives StrictMode effect replay without leaving metadata stuck loading", async () => {
    const wrapper = ({ children }) => <StrictMode>{children}</StrictMode>;
    const { result } = renderConsole({}, { wrapper });

    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    expect(result.current.metadata.tables).toHaveLength(1);
  });

  it("discards metadata returned after the connector target changes", async () => {
    const pending = new Map();
    apiPost.mockImplementation((_path, payload) => new Promise((resolve) => pending.set(payload.target_ref, resolve)));
    const { result, rerender, props } = renderConsole();
    await waitFor(() => expect(pending.has("test-sql:1:1")).toBe(true));

    rerender({ ...props, target: { ...props.target, ref: "test-sql:2:2" } });
    await waitFor(() => expect(pending.has("test-sql:2:2")).toBe(true));
    await act(async () => pending.get("test-sql:1:1")(completed(metadataOutput("stale", "ignored"))));
    expect(result.current.metadata.tables).toEqual([]);

    await act(async () => pending.get("test-sql:2:2")(completed(metadataOutput("current", "events"))));
    await waitFor(() => expect(result.current.metadata.tables[0]).toMatchObject({ schema: "current", table: "events" }));
  });

  it("runs the current SQL against the captured target and keeps the editor text", async () => {
    const { result } = renderConsole();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    apiPost.mockClear();
    act(() => {
      result.current.setSQL("SELECT id FROM users");
      result.current.setMaxRows("25");
    });

    await act(async () => result.current.runQuery());

    expect(apiPost).toHaveBeenCalledWith(
      "/api/connector-actions/local-run",
      {
        target_ref: "test-sql:1:1",
        action_name: "query_readonly",
        input: { sql: "SELECT id FROM users", max_rows: 25 },
        reason: "manual test query",
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(result.current.sql).toBe("SELECT id FROM users");
    expect(result.current.selectedID).toBe(41);
    expect(result.current.runState).toEqual({ state: "idle", error: "" });
  });

  it("ignores a query result returned after the connector target changes", async () => {
    let resolveQuery;
    apiPost.mockImplementation((_path, payload) => {
      if (payload.input.sql === config.metadataSQL) return Promise.resolve(completed(metadataOutput("public", "users")));
      return new Promise((resolve) => (resolveQuery = resolve));
    });
    const { result, rerender, props } = renderConsole();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    act(() => result.current.setSQL("SELECT 1"));
    let queryPromise;
    act(() => {
      queryPromise = result.current.runQuery();
    });
    await waitFor(() => expect(resolveQuery).toBeTypeOf("function"));

    rerender({ ...props, target: { ...props.target, ref: "test-sql:2:2" } });
    await act(async () => {
      resolveQuery(completed({ columns: ["value"], rows: [{ value: 1 }] }));
      await queryPromise;
    });

    expect(result.current.selectedID).toBeNull();
    expect(result.current.runState).toEqual({ state: "idle", error: "" });
  });

  it("reports activity refresh failure without calling a completed query failed", async () => {
    const onRefreshActivity = vi.fn().mockResolvedValue(undefined);
    const { result } = renderConsole({ onRefreshActivity });
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    await waitFor(() => expect(onRefreshActivity).toHaveBeenCalled());
    onRefreshActivity.mockRejectedValueOnce(new Error("activity unavailable"));
    act(() => result.current.setSQL("SELECT 1"));

    await act(async () => result.current.runQuery());

    expect(result.current.runState).toEqual({
      state: "error",
      error: "Query completed, but activity refresh failed: activity unavailable",
    });
  });

  it("handles non-Error query failures without rejecting the UI event", async () => {
    const { result } = renderConsole();
    await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
    apiPost.mockRejectedValueOnce(null);
    act(() => result.current.setSQL("SELECT 1"));

    await act(async () => {
      await result.current.runQuery();
    });
    expect(result.current.runState).toEqual({ state: "error", error: "Query failed." });
  });
});

function completed(output) {
  return { id: 41, request_id: 41, status: "completed", action_name: "query_readonly", output };
}

function metadataOutput(schema, table) {
  return {
    columns: ["table_schema", "table_name", "column_name", "data_type", "ordinal_position"],
    rows: [{ table_schema: schema, table_name: table, column_name: "id", data_type: "integer", ordinal_position: 1 }],
  };
}
