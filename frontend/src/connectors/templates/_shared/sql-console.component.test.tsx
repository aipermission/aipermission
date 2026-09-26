import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { SQLConnectorConsole } from "./sql-console";
import type { SQLEditorProps } from "./sql-editor";
import type { SQLActivityItem, SQLConsoleProps } from "./use-sql-console";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));
vi.mock("./sql-editor", () => ({
  SQLEditor: ({ value, onChange, disabled }: SQLEditorProps) => (
    <textarea aria-label="SQL editor" value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} />
  ),
}));

const item: SQLActivityItem = {
  id: 7,
  target_ref: "sql:1:1",
  action_name: "query_readonly",
  created_at: "2026-01-01T12:01:00Z",
  status: "completed",
  input: { sql: "SELECT 7" },
  output: { columns: ["id"], rows: [{ id: 7 }] },
  reason: "Inspect current request",
};
const props: SQLConsoleProps & { theme: "dark" } = {
  target: { ref: "sql:1:1", name: "Test database" },
  session: { active: true, startedAt: "2026-01-01T12:00:00Z" },
  approvals: { data: [item] },
  theme: "dark",
};

beforeEach(() => {
  vi.mocked(apiPost).mockReset().mockResolvedValue({ status: "completed", output: { rows: [] } });
});

it("keeps inactive sessions behind a start placeholder without fetching metadata", async () => {
  const user = userEvent.setup();
  const onNewSession = vi.fn();
  render(<SQLConnectorConsole {...props} session={null} onNewStructuredSession={onNewSession} />);

  expect(screen.getByText("No active SQL session")).toBeVisible();
  expect(screen.queryByLabelText("SQL editor")).not.toBeInTheDocument();
  expect(apiPost).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "New Session" }));
  expect(onNewSession).toHaveBeenCalledOnce();
});

it("filters session requests and switches between raw input/output and full-width rows", async () => {
  const user = userEvent.setup();
  render(<SQLConnectorConsole {...props} approvals={{ data: [
    item,
    { ...item, id: 8, created_at: "2025-12-31T12:00:00Z", reason: "Archived request" },
    { ...item, id: 9, target_ref: "sql:2:2", reason: "Other target" },
    { ...item, id: 10, reason: "load SQL console autocomplete" },
  ] }} />);

  await waitFor(() => expect(apiPost).toHaveBeenCalledOnce());
  await user.click(screen.getByRole("tab", { name: "Requests" }));
  expect(screen.getByRole("button", { name: /Inspect current request/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.queryByText("Archived request")).not.toBeInTheDocument();
  expect(screen.queryByText("Other target")).not.toBeInTheDocument();
  expect(screen.getByText("Input")).toBeVisible();
  expect(screen.getByText("Output")).toBeVisible();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();

  await user.click(screen.getByRole("switch", { name: "Result View" }));
  expect(screen.getByRole("table")).toBeVisible();
  expect(screen.queryByText("Input")).not.toBeInTheDocument();
  expect(screen.queryByRole("tablist")).not.toBeInTheDocument();

  await user.click(screen.getByRole("switch", { name: "Result View" }));
  await user.click(screen.getByRole("button", { name: "Load SQL" }));
  expect(screen.getByLabelText("SQL editor")).toHaveValue("SELECT 7");
});

it("hides prior requests when a session ends and keeps the endpoint visible", async () => {
  const target = { ...props.target, config: { host: "database.local", port: 5432, database: "app" } };
  const { rerender } = render(<SQLConnectorConsole {...props} target={target} />);
  expect(screen.getByText("Request #7")).toBeVisible();
  await waitFor(() => expect(apiPost).toHaveBeenCalledOnce());

  rerender(<SQLConnectorConsole {...props} target={target} session={{ active: false, startedAt: props.session?.startedAt ?? "" }} />);
  expect(screen.getByText("No active SQL session")).toBeVisible();
  expect(screen.queryByText("Request #7")).not.toBeInTheDocument();
  expect(screen.getByText("database.local:5432/app")).toBeVisible();
});
