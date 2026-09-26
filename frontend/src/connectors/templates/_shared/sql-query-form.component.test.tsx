import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { SQLQueryForm } from "./sql-query-form";
import type { SQLQueryFormProps } from "./sql-query-form";
import type { SQLEditorProps } from "./sql-editor";
import { normalizeSQLConsoleConfig } from "./sql-console-config";
import { connectorConsoleTheme } from "./console-theme";

vi.mock("./sql-editor", () => ({
  SQLEditor: ({ identifierPolicy }: SQLEditorProps) => <div data-testid="sql-editor-policy">{identifierPolicy}</div>,
}));

it("passes the connector identifier policy to the editor and submits SQL", () => {
  const runQuery = vi.fn<SQLQueryFormProps["controller"]["runQuery"]>(async (event) => { event?.preventDefault?.(); });
  const controller: SQLQueryFormProps["controller"] = {
    connector: normalizeSQLConsoleConfig({ label: "ClickHouse", identifierPolicy: "exact" }),
    metadata: { state: "ready", tables: [], error: "", truncated: false },
    runState: { state: "idle", error: "" },
    sql: "SELECT 1",
    setSQL: vi.fn(),
    runQuery,
    editorFocusTick: 0,
    recentQueries: [],
    loadSQL: vi.fn(),
    maxRows: 100,
    setMaxRows: vi.fn(),
  };

  render(<SQLQueryForm controller={controller} styles={connectorConsoleTheme("dark")} theme="dark" />);

  expect(screen.getByTestId("sql-editor-policy")).toHaveTextContent("exact");
  fireEvent.click(screen.getByRole("button", { name: "Run SQL (Ctrl+Enter)" }));
  expect(runQuery).toHaveBeenCalledOnce();
});
