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

it("passes the connector identifier policy to the editor and submits SQL", () => {
  const runQuery = vi.fn<SQLQueryFormProps["controller"]["runQuery"]>(async (event) => {
    event?.preventDefault?.();
  });
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
