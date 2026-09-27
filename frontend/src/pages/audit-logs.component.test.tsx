import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiGet as realGet } from "../lib/api";
import { AuditLogsPage } from "./audit-logs";
import { auditEntryFixture } from "../test/audit-fixtures";
const apiGet = vi.mocked(realGet);

vi.mock("../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: () => ({ targets: { state: "ready", data: [] } }) }));

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<unknown>((next, fail) => {
    resolve = next;
    reject = fail;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.useFakeTimers();
  apiGet.mockReset();
});

it("renders bounded payload fields on the row and preserves formatted raw JSON in details", async () => {
  const payload = {
    request_id: 12,
    command: "printf\n  ok",
    reason: "Smoke test",
    exit_code: 0,
    user_note: "",
    ignored: "not a preview field",
  };
  const entry = auditEntryFixture({ action: "connector.completed", payload_json: JSON.stringify(payload) });
  apiGet.mockImplementation(async (path) => {
    if (path === "/api/projects") return { items: [] };
    if (path === `/api/audit-logs/${entry.id}`) return entry;
    return { items: [entry], total: 1, limit: 50, offset: 0 };
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));
  expect(screen.getByText("request_id: 12, command: printf ok, reason: Smoke test, exit_code: 0")).toBeVisible();
  expect(screen.queryByText("not a preview field")).not.toBeInTheDocument();
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open audit details for connector.completed" })));
  const dialog = screen.getByRole("dialog", { name: "Audit #1" });
  expect(within(dialog).getByText(/"ignored": "not a preview field"/)).toHaveTextContent('"request_id": 12');
});

afterEach(() => {
  vi.useRealTimers();
});

it("reports malformed audit pages without rendering unverified rows", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    return Promise.resolve({ ...auditResponse("unsafe"), items: [{ id: "unsafe" }] });
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));
  expect(screen.getByText("Invalid audit response from gateway.")).toBeVisible();
  expect(screen.queryByText("unsafe")).not.toBeInTheDocument();
});

it("keeps the selected record when a detail response belongs to another audit ID", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path === "/api/audit-logs/1") return Promise.resolve(auditEntryFixture({ id: 2, action: "wrong.record" }));
    return Promise.resolve(auditResponse("opened"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open audit details for opened" })));
  expect(screen.getByRole("dialog", { name: "Audit #1" })).toBeVisible();
  expect(screen.queryByText("wrong.record")).not.toBeInTheDocument();
});

it("ignores an older audit response after the search filter changes", async () => {
  const older = deferred();
  const newer = deferred();
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path.includes("q=current")) return newer.promise;
    return older.promise;
  });
  render(<AuditLogsPage />);

  await act(async () => vi.advanceTimersByTimeAsync(250));
  fireEvent.change(screen.getByPlaceholderText("Search actions, names, or payload"), { target: { value: "current" } });
  await act(async () => vi.advanceTimersByTimeAsync(250));
  await act(async () => newer.resolve(auditResponse("current")));
  expect(screen.getByText("current")).toBeVisible();

  await act(async () => older.resolve(auditResponse("stale")));
  expect(screen.queryByText("stale")).not.toBeInTheDocument();
  expect(screen.getByText("current")).toBeVisible();

  const detailButton = screen.getByRole("button", { name: "Open audit details for current" });
  detailButton.focus();
  fireEvent.click(detailButton);
  expect(screen.getByRole("dialog", { name: "Audit #1" })).toBeVisible();
});

it("does not describe a failed audit request as an empty result", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    return Promise.reject(new Error("audit unavailable"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  expect(screen.getByText("audit unavailable")).toBeVisible();
  expect(screen.queryByText("No audit events match these filters.")).not.toBeInTheDocument();
});

it("names every filter and keeps the audit table horizontally recoverable", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    return Promise.resolve(auditResponse("opened"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  expect(screen.getByRole("textbox", { name: "Search audit logs" })).toBeVisible();
  for (const name of [
    "Filter audit logs by project",
    "Filter audit logs by actor",
    "Filter audit logs by connector type",
    "Filter audit logs by connector",
  ]) {
    expect(screen.getByRole("combobox", { name })).toBeVisible();
  }
  expect(screen.getByTestId("audit-table-scroll")).toHaveClass("overflow-x-auto");
});

it("opens audit details from the whole row", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path === "/api/audit-logs/1") return Promise.resolve(auditResponse("opened").items[0]);
    return Promise.resolve(auditResponse("opened"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  const row = screen.getByText("opened").closest("tr");
  if (!row) throw new Error("Missing audit row");
  fireEvent.click(row.cells[0]);

  expect(screen.getByRole("dialog", { name: "Audit #1" })).toBeVisible();
});

it.each<[string, (_pending: ReturnType<typeof deferred>) => void]>([
  ["success", (pending) => pending.resolve(auditResponse("opened").items[0])],
  ["failure", (pending) => pending.reject(new Error("detail unavailable"))],
])("keeps a dismissed audit dialog closed after late detail %s", async (_outcome, settle) => {
  const detail = deferred();
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path === "/api/audit-logs/1") return detail.promise;
    return Promise.resolve(auditResponse("opened"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  const row = screen.getByText("opened").closest("tr");
  if (!row) throw new Error("Missing audit row");
  fireEvent.click(row);
  fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog", { name: "Audit #1" })).not.toBeInTheDocument();
  await act(async () => settle(detail));

  expect(screen.queryByRole("dialog", { name: "Audit #1" })).not.toBeInTheDocument();
});

it("serializes project, actor, connector type, and runtime target filters", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [{ id: 7, name: "My Project", slug: "my-project", target_count: 1 }] });
    return Promise.resolve({
      ...auditResponse("connector.run"),
      items: [
        auditEntryFixture({
          id: 1,
          actor_type: "mcp",
          action: "connector.run",
          project_id: 7,
          connector_kind: "postgres",
          runtime_id: 11,
          target_name: "Main DB",
          created_at: "2026-09-08T00:00:00Z",
        }),
      ],
    });
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  fireEvent.change(screen.getByRole("combobox", { name: "Filter audit logs by project" }), { target: { value: "7" } });
  fireEvent.change(screen.getByRole("combobox", { name: "Filter audit logs by actor" }), { target: { value: "mcp" } });
  fireEvent.change(screen.getByRole("combobox", { name: "Filter audit logs by connector type" }), {
    target: { value: "postgres" },
  });
  fireEvent.change(screen.getByRole("combobox", { name: "Filter audit logs by connector" }), {
    target: { value: "runtime:11" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: "Search audit logs" }), { target: { value: "  connector  " } });
  await act(async () => vi.advanceTimersByTimeAsync(250));

  const listCalls = apiGet.mock.calls.map(([path]) => path).filter((path) => path.startsWith("/api/audit-logs?"));
  const last = listCalls.at(-1);
  if (!last) throw new Error("Missing audit list request");
  const params = new URL(last, "http://localhost").searchParams;
  expect(Object.fromEntries(params)).toMatchObject({
    q: "connector",
    project_id: "7",
    actor: "mcp",
    connector_kind: "postgres",
    runtime_id: "11",
  });
  expect(params.has("target_id")).toBe(false);
});

it("uses target_ref once when a target name is unavailable", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path === "/api/audit-logs/1") {
      return Promise.resolve(
        auditEntryFixture({
          id: 1,
          actor_type: "mcp",
          action: "connector.run",
          target_ref: "postgres:7:11",
          created_at: "2026-09-08T00:00:00Z",
        }),
      );
    }
    return Promise.resolve({
      ...auditResponse("connector.run"),
      items: [
        auditEntryFixture({
          id: 1,
          actor_type: "mcp",
          action: "connector.run",
          target_ref: "postgres:7:11",
          created_at: "2026-09-08T00:00:00Z",
        }),
      ],
    });
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open audit details for connector.run" })));
  const dialog = screen.getByRole("dialog", { name: "Audit #1" });
  expect(within(dialog).getAllByText("postgres:7:11")).toHaveLength(1);
});

function auditResponse(action: string) {
  return {
    items: [auditEntryFixture({ action, created_at: "2026-09-08T00:00:00Z" })],
    total: 1,
    limit: 50,
    offset: 0,
  };
}
