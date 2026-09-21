import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../lib/api";
import { AuditLogsPage } from "./audit-logs";

vi.mock("../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: () => ({ targets: { state: "ready", data: [] } }) }));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((next, fail) => {
    resolve = next;
    reject = fail;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.useFakeTimers();
  apiGet.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
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
  expect(screen.getByRole("dialog", { name: "Audit #current" })).toBeVisible();
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
    if (path === "/api/audit-logs/opened") return Promise.resolve(auditResponse("opened").items[0]);
    return Promise.resolve(auditResponse("opened"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  const row = screen.getByText("opened").closest("tr");
  fireEvent.click(row.cells[0]);

  expect(screen.getByRole("dialog", { name: "Audit #opened" })).toBeVisible();
});

it.each([
  ["success", (pending) => pending.resolve(auditResponse("opened").items[0])],
  ["failure", (pending) => pending.reject(new Error("detail unavailable"))],
])("keeps a dismissed audit dialog closed after late detail %s", async (_outcome, settle) => {
  const detail = deferred();
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path === "/api/audit-logs/opened") return detail.promise;
    return Promise.resolve(auditResponse("opened"));
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  fireEvent.click(screen.getByText("opened").closest("tr"));
  fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog", { name: "Audit #opened" })).not.toBeInTheDocument();
  await act(async () => settle(detail));

  expect(screen.queryByRole("dialog", { name: "Audit #opened" })).not.toBeInTheDocument();
});

it("serializes project, actor, connector type, and runtime target filters", async () => {
  apiGet.mockImplementation((path) => {
    if (path === "/api/projects") return Promise.resolve({ items: [{ id: 7, name: "My Project" }] });
    return Promise.resolve({
      ...auditResponse("connector.run"),
      items: [
        {
          id: "runtime-event",
          actor_type: "mcp",
          action: "connector.run",
          project_id: 7,
          connector_kind: "postgres",
          runtime_id: 11,
          target_name: "Main DB",
          created_at: "2026-09-08T00:00:00Z",
        },
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
  const params = new URL(listCalls.at(-1), "http://localhost").searchParams;
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
    if (path === "/api/audit-logs/fallback") {
      return Promise.resolve({
        id: "fallback",
        actor_type: "mcp",
        action: "connector.run",
        target_ref: "postgres:7:11",
        created_at: "2026-09-08T00:00:00Z",
      });
    }
    return Promise.resolve({
      ...auditResponse("connector.run"),
      items: [
        {
          id: "fallback",
          actor_type: "mcp",
          action: "connector.run",
          target_ref: "postgres:7:11",
          created_at: "2026-09-08T00:00:00Z",
        },
      ],
    });
  });
  render(<AuditLogsPage />);
  await act(async () => vi.advanceTimersByTimeAsync(250));

  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Open audit details for connector.run" })));
  const dialog = screen.getByRole("dialog", { name: "Audit #fallback" });
  expect(within(dialog).getAllByText("postgres:7:11")).toHaveLength(1);
});

function auditResponse(action) {
  return {
    items: [{ id: action, actor_type: "user", action, created_at: "2026-09-08T00:00:00Z" }],
    total: 1,
    limit: 50,
    offset: 0,
    next_offset: null,
  };
}
