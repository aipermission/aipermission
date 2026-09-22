import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { SQLQueryForm } from "./sql-query-form";

vi.mock("./sql-editor", () => ({
  SQLEditor: ({ identifierPolicy }) => <div data-testid="sql-editor-policy">{identifierPolicy}</div>,
}));

it("passes the connector identifier policy to the editor and submits SQL", () => {
  const runQuery = vi.fn((event) => event.preventDefault());
  const controller = {
    connector: { label: "ClickHouse", identifierPolicy: "exact" },
    metadata: { state: "ready", tables: [], error: "", truncated: false },
    runState: { state: "idle", error: "" },
    sql: "SELECT 1",
    setSQL: vi.fn(),
    runQuery,
    editorFocusTick: 0,
    recentQueries: [],
    maxRows: 100,
    setMaxRows: vi.fn(),
  };

  render(<SQLQueryForm controller={controller} styles={{ border: "", subtlePanel: "", muted: "", input: "" }} theme="dark" />);

  expect(screen.getByTestId("sql-editor-policy")).toHaveTextContent("exact");
  fireEvent.click(screen.getByRole("button", { name: "Run SQL (Ctrl+Enter)" }));
  expect(runQuery).toHaveBeenCalledOnce();
});
