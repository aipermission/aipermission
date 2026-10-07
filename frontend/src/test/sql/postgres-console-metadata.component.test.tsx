import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { PostgresConnectorConsoleTemplate } from "../../connectors/templates/postgres/console";
import { connectorActionFixture, connectorActionRequest } from "../connector-action-fixtures";
import { consoleWorkspaceFixture } from "../console-workspace-fixtures";
import { gatewayTargetFixture } from "../connector-inventory-fixtures";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));
vi.mock("../../connectors/templates/_shared/sql-editor", () => ({ SQLEditor: () => <div /> }));

beforeEach(() => {
  vi.mocked(apiPost)
    .mockReset()
    .mockImplementation(async (_path, payload) => {
      const action = connectorActionRequest(payload);
      if (action.action_name === "query_readonly") throw new Error("explicit casts are not allowed");
      return connectorActionFixture({
        action_name: action.action_name,
        output: {
          rows:
            action.action_name === "get_tables"
              ? [{ table_schema: "public", table_name: "users", table_type: "BASE TABLE" }]
              : [
                  { table_schema: "public", table_name: "users", column_name: "id", data_type: "integer", ordinal_position: 1 },
                  { table_schema: "public", table_name: "users", column_name: "email", data_type: "text", ordinal_position: 2 },
                ],
        },
      });
    });
});

function postgresProps(startedAt = "2026-10-08T00:00:00Z") {
  return consoleWorkspaceFixture({
    target: gatewayTargetFixture({ connector_kind: "postgres", ref: "postgres:3:11" }),
    session: { active: true, startedAt },
  });
}

function renderPostgres() {
  return render(<PostgresConnectorConsoleTemplate {...postgresProps()} />);
}

it("discovers tables before any SQL is typed without submitting catalog SQL to query_readonly", async () => {
  renderPostgres();
  expect(await screen.findByTitle("Show columns for public.users")).toBeVisible();
  expect(apiPost).toHaveBeenCalledWith(
    "/api/connector-actions/local-run",
    {
      target_ref: "postgres:3:11",
      action_name: "get_tables",
      input: {},
      reason: "load Postgres console autocomplete",
    },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(apiPost).toHaveBeenCalledOnce();
});

it("loads exact ordered columns on expansion without preparing or executing a query", async () => {
  const user = userEvent.setup();
  renderPostgres();
  await user.click(await screen.findByTitle("Show columns for public.users"));
  await waitFor(() => expect(screen.getByText("email")).toBeVisible());
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-actions/local-run",
    {
      target_ref: "postgres:3:11",
      action_name: "describe_table",
      input: { schema: "public", table: "users" },
      reason: "load Postgres console autocomplete",
    },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(screen.getByText("id").compareDocumentPosition(screen.getByText("email")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  await user.click(screen.getByTitle("Hide columns for public.users"));
  await user.click(screen.getByTitle("Show columns for public.users"));
  expect(apiPost).toHaveBeenCalledTimes(2);
  expect(screen.getByRole("button", { name: "Run SQL (Ctrl+Enter)" })).toBeDisabled();
});

it("resets expansion and reloads columns when starting another session", async () => {
  const user = userEvent.setup();
  const view = renderPostgres();
  await user.click(await screen.findByTitle("Show columns for public.users"));
  expect(await screen.findByText("email")).toBeVisible();
  view.rerender(<PostgresConnectorConsoleTemplate {...postgresProps("2026-10-08T01:00:00Z")} />);
  expect(await screen.findByTitle("Show columns for public.users")).toBeVisible();
  expect(screen.queryByText("email")).not.toBeInTheDocument();
  await user.click(screen.getByTitle("Show columns for public.users"));
  expect(await screen.findByText("email")).toBeVisible();
  expect(apiPost).toHaveBeenCalledTimes(4);
});
