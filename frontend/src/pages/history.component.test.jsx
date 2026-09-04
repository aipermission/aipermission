import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "../lib/api";
import { HistoryPage } from "./history";

vi.mock("../lib/api", () => ({
  apiDelete: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function historyResponse(name, overrides = {}) {
  return {
    items: [
      {
        id: overrides.id || name,
        status: overrides.status || "completed",
        connector_kind: "ssh",
        target_name: name,
        action: "exec",
        created_at: "2026-09-05T12:00:00Z",
      },
    ],
    total: overrides.total || 1,
    limit: 50,
    has_more: Boolean(overrides.nextCursor),
    next_cursor: overrides.nextCursor || null,
  };
}

function installHistoryMock(responses = {}) {
  apiGet.mockImplementation((path) => {
    if (path === "/api/history-labels") return Promise.resolve([]);
    if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
    if (typeof path !== "string" || !path.startsWith("/api/history?")) return Promise.resolve({});
    const query = new URL(path, "http://localhost").searchParams.get("q") || "initial";
    return responses[query]?.promise || Promise.resolve(historyResponse(query));
  });
}

async function waitForHistoryRequest(query) {
  await waitFor(() =>
    expect(
      apiGet.mock.calls.some(
        ([path]) =>
          typeof path === "string" && path.startsWith("/api/history?") && new URL(path, "http://localhost").searchParams.get("q") === query,
      ),
    ).toBe(true),
  );
}

describe("HistoryPage request ownership", () => {
  beforeEach(() => apiGet.mockReset());

  it("ignores an older filter response that resolves after the current result", async () => {
    const older = deferred();
    const current = deferred();
    installHistoryMock({ older, current });
    render(<HistoryPage />);

    expect(await screen.findByText("initial")).toBeVisible();
    const search = screen.getByPlaceholderText("Search targets, actions, output, paths, or tokens");
    fireEvent.change(search, { target: { value: "older" } });
    await waitForHistoryRequest("older");
    fireEvent.change(search, { target: { value: "current" } });
    await waitForHistoryRequest("current");

    await act(async () => current.resolve(historyResponse("current", { total: 7, nextCursor: "current-next" })));
    expect(await screen.findByText("current")).toBeVisible();
    await act(async () => older.resolve(historyResponse("older", { total: 99, nextCursor: "older-next" })));

    expect(screen.queryByText("older")).not.toBeInTheDocument();
    expect(screen.getByText("current")).toBeVisible();
    expect(within(screen.getByText("Total").parentElement).getByText("7")).toBeVisible();
  });

  it("invalidates an in-flight filter response before the debounced replacement starts", async () => {
    const older = deferred();
    const current = deferred();
    installHistoryMock({ older, current });
    render(<HistoryPage />);

    expect(await screen.findByText("initial")).toBeVisible();
    const search = screen.getByPlaceholderText("Search targets, actions, output, paths, or tokens");
    fireEvent.change(search, { target: { value: "older" } });
    await waitForHistoryRequest("older");
    fireEvent.change(search, { target: { value: "current" } });
    await act(async () => older.resolve(historyResponse("older")));

    expect(screen.queryByText("older")).not.toBeInTheDocument();
    await waitForHistoryRequest("current");
    await act(async () => current.resolve(historyResponse("current")));
    expect(await screen.findByText("current")).toBeVisible();
  });

  it("ignores an older filter failure after the current result succeeds", async () => {
    const older = deferred();
    const current = deferred();
    installHistoryMock({ older, current });
    render(<HistoryPage />);

    expect(await screen.findByText("initial")).toBeVisible();
    const search = screen.getByPlaceholderText("Search targets, actions, output, paths, or tokens");
    fireEvent.change(search, { target: { value: "older" } });
    await waitForHistoryRequest("older");
    fireEvent.change(search, { target: { value: "current" } });
    await waitForHistoryRequest("current");

    await act(async () => current.resolve(historyResponse("current")));
    expect(await screen.findByText("current")).toBeVisible();
    await act(async () => older.reject(new Error("stale request failed")));

    expect(screen.queryByText("stale request failed")).not.toBeInTheDocument();
    expect(screen.getByText("current")).toBeVisible();
  });

  it("does not reopen a closed detail dialog when its request resolves late", async () => {
    const detail = deferred();
    installHistoryMock();
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (path === "/api/history/initial") return detail.promise;
      if (typeof path === "string" && path.startsWith("/api/history?")) return Promise.resolve(historyResponse("initial"));
      return Promise.resolve({});
    });
    render(<HistoryPage />);

    fireEvent.click(await screen.findByText("initial"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close dialog" }));
    await act(async () => detail.resolve({ ...historyResponse("detail").items[0], id: "initial" }));

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("serializes slow polling and eventually commits its response", { timeout: 10000 }, async () => {
    const poll = deferred();
    let historyCalls = 0;
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (typeof path !== "string" || !path.startsWith("/api/history?")) return Promise.resolve({});
      historyCalls++;
      if (historyCalls === 1) return Promise.resolve(historyResponse("active entry", { status: "running" }));
      return poll.promise;
    });
    render(<HistoryPage />);

    expect(await screen.findByText("active entry")).toBeVisible();
    await waitFor(() => expect(historyCalls).toBe(2), { timeout: 2500 });
    await new Promise((resolve) => window.setTimeout(resolve, 1650));
    expect(historyCalls).toBe(2);
    await act(async () => poll.resolve(historyResponse("polled")));
    expect(await screen.findByText("polled")).toBeVisible();
  });

  it("does not let an older poll overwrite a manual refresh", { timeout: 10000 }, async () => {
    const poll = deferred();
    const refresh = deferred();
    let historyCalls = 0;
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (typeof path !== "string" || !path.startsWith("/api/history?")) return Promise.resolve({});
      historyCalls++;
      if (historyCalls === 1) return Promise.resolve(historyResponse("active entry", { status: "running" }));
      if (historyCalls === 2) return poll.promise;
      return refresh.promise;
    });
    render(<HistoryPage />);

    expect(await screen.findByText("active entry")).toBeVisible();
    await waitFor(() => expect(historyCalls).toBe(2), { timeout: 2500 });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(historyCalls).toBe(3));
    await act(async () => refresh.resolve(historyResponse("refreshed")));
    expect(await screen.findByText("refreshed")).toBeVisible();
    await act(async () => poll.resolve(historyResponse("stale poll")));
    expect(screen.queryByText("stale poll")).not.toBeInTheDocument();
    expect(screen.getByText("refreshed")).toBeVisible();
  });

  it("never combines a changed filter with the previous page cursor", { timeout: 10000 }, async () => {
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (typeof path !== "string" || !path.startsWith("/api/history?")) return Promise.resolve({});
      const params = new URL(path, "http://localhost").searchParams;
      if (params.get("cursor") === "page-2") return Promise.resolve(historyResponse("page two", { status: "running" }));
      if (params.get("q") === "changed") return Promise.resolve(historyResponse("changed"));
      return Promise.resolve(historyResponse("page one", { nextCursor: "page-2" }));
    });
    render(<HistoryPage />);

    expect(await screen.findByText("page one")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("page two")).toBeVisible();
    await new Promise((resolve) => window.setTimeout(resolve, 1350));
    fireEvent.change(screen.getByPlaceholderText("Search targets, actions, output, paths, or tokens"), {
      target: { value: "changed" },
    });
    await waitForHistoryRequest("changed");

    const changedRequests = apiGet.mock.calls
      .map(([path]) => path)
      .filter((path) => typeof path === "string" && path.startsWith("/api/history?"))
      .map((path) => new URL(path, "http://localhost").searchParams)
      .filter((params) => params.get("q") === "changed");
    expect(changedRequests.length).toBeGreaterThan(0);
    expect(changedRequests.every((params) => !params.has("cursor"))).toBe(true);
  });
});
