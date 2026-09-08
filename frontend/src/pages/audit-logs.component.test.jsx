import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../lib/api";
import { AuditLogsPage } from "./audit-logs";

vi.mock("../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../lib/gateway-context", () => ({ useGateway: () => ({ targets: { state: "ready", data: [] } }) }));

function deferred() {
  let resolve;
  const promise = new Promise((next) => {
    resolve = next;
  });
  return { promise, resolve };
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

function auditResponse(action) {
  return {
    items: [{ id: action, actor_type: "user", action, created_at: "2026-09-08T00:00:00Z" }],
    total: 1,
    limit: 50,
    offset: 0,
    next_offset: null,
  };
}
