import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { SQLConnectorConsole } from "../../connectors/templates/_shared/sql-console";
import { useSQLConsole } from "../../connectors/templates/_shared/use-sql-console";
import { PostgresConnectorConsoleTemplate } from "../../connectors/templates/postgres/console";
import { SQLServerConnectorConsoleTemplate } from "../../connectors/templates/sqlserver/console";
import { ClickHouseConnectorConsoleTemplate } from "../../connectors/templates/clickhouse/console";
import { consoleWorkspaceFixture } from "../console-workspace-fixtures";
import { executionProps, executionResponse, setupSQLExecutionTransport } from "./execution-test-state";
import type { SQLConsoleProps } from "../../connectors/templates/_shared/use-sql-console";
import type { SQLEditorProps } from "../../connectors/templates/_shared/sql-editor";

vi.mock("../../connectors/templates/_shared/sql-editor", () => ({
  SQLEditor: ({ value, onChange, onSubmit, disabled, keywords }: SQLEditorProps) => (
    <textarea
      aria-label="SQL editor"
      data-keywords={keywords?.join(",")}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      disabled={disabled}
      onKeyDown={(event) => {
        if (event.ctrlKey && event.key === "Enter") void onSubmit?.();
      }}
    />
  ),
}));
const transport = setupSQLExecutionTransport();

function hook(overrides: Partial<SQLConsoleProps> = {}) {
  return renderHook((props: SQLConsoleProps) => useSQLConsole(props), { initialProps: { ...executionProps, ...overrides } });
}

it("defaults to the unchanged read-only request and never infers execution from SQL text", async () => {
  const { result } = hook();
  await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
  expect(result.current.executionMode).toBe(false);
  act(() => result.current.setSQL("DELETE FROM users"));
  await act(async () => result.current.runQuery());
  expect(transport.posts).toHaveBeenLastCalledWith(
    expect.objectContaining({
      action_name: "query_readonly",
      input: { sql: "DELETE FROM users", max_rows: 100 },
      reason: "manual Test SQL console query",
    }),
    expect.any(Object),
  );
  expect(transport.execute).not.toHaveBeenCalled();
});

it("uses explicit execution permission even for SELECT, preserving editor and bounded inputs", async () => {
  const { result } = hook();
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  act(() => {
    result.current.setExecutionMode(true);
    result.current.setSQL("SELECT 1");
    result.current.setMaxRows("25");
    result.current.setTimeoutSeconds("120");
  });
  await act(async () => result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({
      action_name: "execute_sql",
      input: { sql: "SELECT 1", max_rows: 25, timeout_seconds: 120 },
      idempotency_key: expect.any(String),
    }),
  );
  expect(result.current.sql).toBe("SELECT 1");
  expect(result.current.selectedID).toBe(81);
});

it.each([1, 300])("accepts execution timeout boundary %s", async (seconds) => {
  const { result } = hook();
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  act(() => {
    result.current.setExecutionMode(true);
    result.current.setSQL("SELECT 1");
    result.current.setTimeoutSeconds(seconds);
  });
  await act(async () => result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledWith(
    expect.objectContaining({ input: { sql: "SELECT 1", max_rows: 100, timeout_seconds: seconds } }),
  );
});

it.each([0, 301, 1.5, ""])("rejects invalid execution timeout %j without dispatch", async (seconds) => {
  const { result } = hook();
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  act(() => {
    result.current.setExecutionMode(true);
    result.current.setSQL("SELECT 1");
    result.current.setTimeoutSeconds(seconds);
  });
  await act(async () => result.current.runQuery());
  expect(transport.execute).not.toHaveBeenCalled();
  expect(result.current.runState.error).toContain("1-300");
});

it.each([0, 1001, 1.5, ""])("rejects invalid returned-row cap %j without modifying the script", async (rows) => {
  const { result } = hook();
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  act(() => {
    result.current.setExecutionMode(true);
    result.current.setSQL("UPDATE users SET enabled = true");
    result.current.setMaxRows(rows);
  });
  await act(async () => result.current.runQuery());
  expect(transport.execute).not.toHaveBeenCalled();
  expect(result.current.sql).toBe("UPDATE users SET enabled = true");
});

it.each(["target", "profile", "session", "inactive", "action"])(
  "resets to read-only when %s changes and stays reset on return",
  async (change) => {
    const { result, rerender } = hook();
    await waitFor(() => expect(result.current.executionLocked).toBe(false));
    act(() => {
      result.current.setExecutionMode(true);
      result.current.setTimeoutSeconds(90);
      result.current.setSQL("SELECT 42");
    });
    const next: SQLConsoleProps = { ...executionProps };
    if (change === "target") next.target = { ref: "sql-test:2:1", name: "Other target" };
    if (change === "profile") next.target = { ref: "sql-test:1:2", name: "Other profile" };
    if (change === "session") next.session = { active: true, startedAt: "2026-01-02T12:00:00Z" };
    if (change === "inactive") next.session = null;
    if (change === "action") next.config = { ...executionProps.config, executionAction: undefined };
    rerender(next);
    expect(result.current.executionMode).toBe(false);
    expect(result.current.timeoutSeconds).toBe(30);
    rerender(executionProps);
    expect(result.current.executionMode).toBe(false);
    expect(result.current.sql).toBe("SELECT 42");
  },
);

it("prepares table SQL in read-only mode while metadata and column discovery retain their read actions", async () => {
  const { result } = hook();
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  act(() => result.current.setExecutionMode(true));
  await act(async () => result.current.requestTableColumns({ schema: "public", table: "users" }));
  await waitFor(() => expect(transport.posts.mock.calls.map(([body]) => body.action_name)).toContain("describe_table"));
  act(() => result.current.prepareTableQuery({ schema: "public", table: "users" }));
  expect(result.current.executionMode).toBe(false);
  expect(result.current.sql).toContain("SELECT *");
  expect(transport.posts.mock.calls.map(([body]) => body.action_name)).toEqual(["get_metadata", "describe_table"]);
  await act(async () => result.current.runQuery());
  expect(transport.posts.mock.calls.at(-1)?.[0].action_name).toBe("query_readonly");
});

it.each([
  [PostgresConnectorConsoleTemplate, ["call", "do"]],
  [SQLServerConnectorConsoleTemplate, ["exec", "go", "create procedure"]],
] as const)("opts in native console configuration without per-connector state", async (Console, dialectKeywords) => {
  render(<Console {...consoleWorkspaceFixture({ session: executionProps.session! })} />);
  expect(screen.getByRole("radio", { name: "Read-only" })).toBeChecked();
  expect(screen.getByRole("radio", { name: "Execute SQL" })).not.toBeChecked();
  const keywords = screen.getByLabelText("SQL editor").getAttribute("data-keywords")?.split(",");
  expect(keywords).toEqual(
    expect.arrayContaining(["insert into", "update", "delete from", "create", "alter", "drop", "commit", "rollback", ...dialectKeywords]),
  );
});

it("keeps ClickHouse read-only with no mode selector or execution timeout", async () => {
  render(<ClickHouseConnectorConsoleTemplate {...consoleWorkspaceFixture({ session: executionProps.session! })} />);
  await waitFor(() => expect(transport.posts).toHaveBeenCalled());
  expect(screen.queryByRole("radio")).not.toBeInTheDocument();
  expect(screen.getByLabelText("SQL editor").getAttribute("data-keywords")?.split(",")).not.toContain("update");
  expect(screen.queryByRole("spinbutton", { name: "Timeout (seconds)" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Run SQL (Ctrl+Enter)" })).toBeVisible();
});

it("shows destructive risk, timeout and returned-row controls only after explicit selection", async () => {
  const user = userEvent.setup();
  render(<SQLConnectorConsole {...executionProps} theme="light" />);
  await waitFor(() => expect(screen.queryByText(/locked while checking/)).not.toBeInTheDocument());
  await user.type(screen.getByLabelText("SQL editor"), "SELECT 1");
  await user.click(screen.getByRole("radio", { name: "Execute SQL" }));
  expect(screen.getByText(/permanently change or delete/)).toBeVisible();
  expect(screen.getByRole("spinbutton", { name: "Timeout (seconds)" })).toHaveValue(30);
  fireEvent.change(screen.getByRole("spinbutton", { name: "Timeout (seconds)" }), { target: { value: "60" } });
  fireEvent.change(screen.getByRole("spinbutton", { name: "Returned-row cap" }), { target: { value: "12" } });
  await user.click(screen.getByRole("button", { name: "Execute SQL" }));
  await waitFor(() =>
    expect(transport.execute).toHaveBeenCalledWith(
      expect.objectContaining({ input: { sql: "SELECT 1", timeout_seconds: 60, max_rows: 12 } }),
    ),
  );
});

it("reports completed execution and request identity even if the activity refresh fails", async () => {
  const onRefreshActivity = vi.fn().mockResolvedValue(undefined);
  const { result } = hook({ onRefreshActivity });
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  await waitFor(() => expect(onRefreshActivity).toHaveBeenCalled());
  onRefreshActivity.mockRejectedValue(new Error("refresh offline"));
  transport.execute.mockResolvedValue(executionResponse({ display_text: "Script completed." }));
  act(() => {
    result.current.setExecutionMode(true);
    result.current.setSQL("SELECT 1");
  });
  await act(async () => result.current.runQuery());
  expect(result.current.runState.error).toContain("Action completed, but activity refresh failed");
  expect(result.current.selectedID).toBe(81);
  expect(transport.execute).toHaveBeenCalledOnce();
});
