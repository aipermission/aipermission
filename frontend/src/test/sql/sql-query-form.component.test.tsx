import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { SQLQueryForm } from "../../connectors/templates/_shared/sql-query-form";
import type { SQLQueryFormProps } from "../../connectors/templates/_shared/sql-query-form";
import type { SQLEditorProps } from "../../connectors/templates/_shared/sql-editor";
import { normalizeSQLConsoleConfig } from "../../connectors/templates/_shared/sql-console-config";
import { connectorConsoleTheme } from "../../connectors/templates/_shared/console-theme";

vi.mock("../../connectors/templates/_shared/sql-editor", () => ({
  SQLEditor: ({ identifierPolicy }: SQLEditorProps) => <div data-testid="sql-editor-policy">{identifierPolicy}</div>,
}));

it.each([
  ["Postgres", "lowercase-unquoted", "dark"],
  ["ClickHouse", "exact", "light"],
] as const)("keeps %s query controls usable and passes the identifier policy", (label, identifierPolicy, theme) => {
  const runQuery = vi.fn<SQLQueryFormProps["controller"]["runQuery"]>(async (event) => {
    event?.preventDefault?.();
  });
  const controller: SQLQueryFormProps["controller"] = {
    connector: normalizeSQLConsoleConfig({ label, identifierPolicy }),
    metadata: { state: "ready", tables: [], error: "", truncated: false },
    runState: { state: "idle", error: "" },
    sql: "SELECT 1",
    setSQL: vi.fn(),
    runQuery,
    editorFocusTick: 0,
    recentQueries: [{ id: 1, sql: "SELECT 2", preview: "SELECT 2", createdAt: "2026-09-07T12:00:00Z" }],
    loadSQL: vi.fn(),
    maxRows: 100,
    setMaxRows: vi.fn(),
  };

  const { rerender } = render(<SQLQueryForm controller={controller} styles={connectorConsoleTheme(theme)} theme={theme} />);

  expect(screen.getByTestId("sql-editor-policy")).toHaveTextContent(identifierPolicy);
  expect(screen.getByRole("button", { name: "SQL" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Last query" }));
  fireEvent.click(screen.getByRole("button", { name: "SELECT 2" }));
  expect(controller.loadSQL).toHaveBeenCalledTimes(2);
  expect(controller.loadSQL).toHaveBeenCalledWith("SELECT 2");
  fireEvent.change(screen.getByRole("spinbutton", { name: "Max rows" }), { target: { value: "25" } });
  expect(controller.setMaxRows).toHaveBeenCalledWith("25");
  fireEvent.click(screen.getByRole("button", { name: "Run SQL (Ctrl+Enter)" }));
  expect(runQuery).toHaveBeenCalledOnce();

  rerender(
    <SQLQueryForm
      controller={{ ...controller, runState: { state: "running", error: "" } }}
      styles={connectorConsoleTheme(theme)}
      theme={theme}
    />,
  );
  expect(screen.getByRole("button", { name: "Running" })).toBeDisabled();
  expect(screen.getByRole("spinbutton", { name: "Max rows" })).toBeDisabled();

  rerender(<SQLQueryForm controller={{ ...controller, sql: "", recentQueries: [] }} styles={connectorConsoleTheme(theme)} theme={theme} />);
  expect(screen.getByRole("button", { name: "SQL" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Run SQL (Ctrl+Enter)" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Last query" })).not.toBeInTheDocument();
});
