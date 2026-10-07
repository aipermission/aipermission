import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { useSQLConsole } from "../../connectors/templates/_shared/use-sql-console";
import type { SQLConsoleProps } from "../../connectors/templates/_shared/use-sql-console";
import { connectorActionFixture, connectorActionRequest } from "../connector-action-fixtures";

// async-owner: src/connectors/templates/_shared/use-sql-metadata.ts

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));
const post = vi.mocked(apiPost);
const table = { schema: " Public ", table: " Users.With.Dot " };
const listed = { rows: [{ table_schema: table.schema, table_name: table.table }] };
const columns = { rows: [{ ...listed.rows[0], column_name: " ID ", data_type: "integer", ordinal_position: 1 }] };

function completed(output: unknown) {
  return connectorActionFixture({ output });
}

function pending() {
  let resolve!: (_response: ReturnType<typeof completed>) => void;
  const promise = new Promise<ReturnType<typeof completed>>((done) => (resolve = done));
  return { promise, resolve };
}

function renderConsole() {
  const props: SQLConsoleProps = {
    config: { metadataAction: "get_tables", metadataInput: {} },
    target: { ref: "test-sql:1:1", name: "Test database" },
    session: { active: true, startedAt: "2026-10-08T00:00:00Z" },
  };
  return { ...renderHook((next: SQLConsoleProps) => useSQLConsole(next), { initialProps: props }), props };
}

beforeEach(() => {
  post
    .mockReset()
    .mockImplementation(async (_path, payload) =>
      completed(connectorActionRequest(payload).action_name === "get_tables" ? listed : columns),
    );
});

it("preserves exact identifiers and deduplicates expansion requests", async () => {
  const hook = renderConsole();
  await waitFor(() => expect(hook.result.current.metadata.state).toBe("ready"));
  act(() => hook.result.current.requestTableColumns(table));
  await waitFor(() => expect(hook.result.current.browserTables[0]?.columnCount).toBe(1));
  expect(connectorActionRequest(post.mock.calls[1][1]).input).toEqual(table);
  act(() => hook.result.current.requestTableColumns(table));
  expect(post).toHaveBeenCalledTimes(2);
  expect(hook.result.current.sql).toBe("");
});

it.each(["target", "session", "config"])("discards late columns after changing the %s owner", async (change) => {
  const hook = renderConsole();
  await waitFor(() => expect(hook.result.current.metadata.state).toBe("ready"));
  const old = pending();
  post.mockImplementationOnce(() => old.promise);
  act(() => hook.result.current.requestTableColumns(table));
  const retainedCallback = hook.result.current.requestTableColumns;
  const next = {
    ...hook.props,
    ...(change === "target" ? { target: { ...hook.props.target, ref: "test-sql:2:2" } } : {}),
    ...(change === "session" ? { session: { active: true, startedAt: "2026-10-08T01:00:00Z" } } : {}),
    ...(change === "config" ? { config: { ...hook.props.config, metadataReason: "new configuration" } } : {}),
  };
  hook.rerender(next);
  await waitFor(() => expect(hook.result.current.metadata.state).toBe("ready"));
  await act(async () => old.resolve(completed(columns)));
  expect(hook.result.current.browserTables[0]?.columnCount).toBe(0);
  act(() => retainedCallback(table));
  expect(post).toHaveBeenCalledTimes(3);
  act(() => hook.result.current.requestTableColumns(table));
  await waitFor(() => expect(hook.result.current.browserTables[0]?.columnCount).toBe(1));
});

it("keeps columns loaded before the initial table list completes", async () => {
  const initial = pending();
  post.mockImplementationOnce(() => initial.promise);
  const hook = renderConsole();
  act(() => hook.result.current.requestTableColumns(table));
  await waitFor(() => expect(hook.result.current.browserTables[0]?.columnCount).toBe(1));
  await act(async () => initial.resolve(completed(listed)));
  expect(hook.result.current.browserTables[0]?.columns[0]?.name).toBe(" ID ");
  expect(hook.result.current.metadata.state).toBe("ready");
});

it("allows retrying a failed column lookup without running SQL", async () => {
  const hook = renderConsole();
  await waitFor(() => expect(hook.result.current.metadata.state).toBe("ready"));
  post.mockRejectedValueOnce(new Error("metadata unavailable"));
  await act(async () => hook.result.current.requestTableColumns(table));
  act(() => hook.result.current.requestTableColumns(table));
  await waitFor(() => expect(hook.result.current.browserTables[0]?.columnCount).toBe(1));
  expect(post).toHaveBeenCalledTimes(3);
  expect(hook.result.current.sql).toBe("");
});

it("does not request metadata without an active session", () => {
  const hook = renderConsole();
  hook.rerender({ ...hook.props, session: { active: false, startedAt: "" } });
  post.mockClear();
  act(() => hook.result.current.requestTableColumns(table));
  expect(post).not.toHaveBeenCalled();
});

it("reloads columns for retained SQL when another session starts", async () => {
  const hook = renderConsole();
  await waitFor(() => expect(hook.result.current.metadata.state).toBe("ready"));
  act(() => hook.result.current.setSQL('SELECT * FROM " Public "." Users.With.Dot "'));
  await waitFor(() => expect(hook.result.current.browserTables[0]?.columnCount).toBe(1));
  hook.rerender({ ...hook.props, session: { active: true, startedAt: "2026-10-08T02:00:00Z" } });
  await waitFor(() => expect(post).toHaveBeenCalledTimes(4));
  expect(connectorActionRequest(post.mock.calls[3][1]).input).toEqual(table);
  expect(hook.result.current.sql).toBe('SELECT * FROM " Public "." Users.With.Dot "');
});
