import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet as realGet } from "../../lib/api";
import { HistoryPage } from "../../pages/history";
import { historyEntryFixture } from "../history-fixtures";
import type { HistoryEntry } from "../../lib/gateway-contracts/history-resource-contract";

vi.mock("../../lib/api", () => ({
  apiDelete: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}));
const apiGet = vi.mocked(realGet);

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<unknown>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function historyResponse(
  name: string,
  overrides: { id?: number; status?: HistoryEntry["status"]; total?: number; nextCursor?: string } = {},
) {
  return {
    items: [
      historyEntryFixture({
        id: overrides.id || 1,
        status: overrides.status || "completed",
        connector_kind: "ssh",
        target_name: name,
        action_name: "exec",
        created_at: "2026-09-05T12:00:00Z",
      }),
    ],
    total: overrides.total || 1,
    limit: 50,
    has_more: Boolean(overrides.nextCursor),
    ...(overrides.nextCursor ? { next_cursor: overrides.nextCursor } : {}),
  };
}

function installHistoryMock(responses: Record<string, ReturnType<typeof deferred>> = {}) {
  apiGet.mockImplementation((path) => {
    if (path === "/api/history-labels") return Promise.resolve([]);
    if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
    if (typeof path !== "string" || !path.startsWith("/api/history?")) return Promise.resolve({});
    const query = new URL(path, "http://localhost").searchParams.get("q") || "initial";
    return responses[query]?.promise || Promise.resolve(historyResponse(query));
  });
}

async function waitForHistoryRequest(query: string) {
  await waitFor(() =>
    expect(
      apiGet.mock.calls.some(
        ([path]) =>
          typeof path === "string" && path.startsWith("/api/history?") && new URL(path, "http://localhost").searchParams.get("q") === query,
      ),
    ).toBe(true),
  );
}

describe("HistoryPage validated references", () => {
  beforeEach(() => {
    apiGet.mockReset();
  });

  it("shows independent label, project and target reference failures without losing a valid history page", async () => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/history-labels") throw new Error("Label reference unavailable");
      if (path === "/api/projects") throw new Error("Project reference unavailable");
      if (path === "/api/history/targets") throw new Error("Target reference unavailable");
      return historyResponse("Available history");
    });
    render(<HistoryPage />);
    expect(await screen.findByText("Available history")).toBeVisible();
    expect(screen.getByText("Label reference unavailable")).toBeVisible();
    expect(screen.getByText("Project reference unavailable")).toBeVisible();
    expect(screen.getByText("Target reference unavailable")).toBeVisible();
  });

  it("shows loading before an empty verified page and disables unavailable pagination", async () => {
    const list = deferred();
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/history-labels") return [];
      if (path === "/api/projects" || path === "/api/history/targets") return { items: [] };
      return list.promise;
    });
    render(<HistoryPage />);
    expect(await screen.findByText("Loading history...")).toBeVisible();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
    await act(async () => list.resolve({ items: [], total: 0, limit: 50, has_more: false }));
    expect(await screen.findByText("No history yet.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled();
  });

  it("keeps a nameless verified target keyboard-accessible and labels unavailable metadata clearly", async () => {
    const entry = historyEntryFixture({ target_name: "", profile_label: "", project_name: "", labels: [] });
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/history-labels") return [];
      if (path === "/api/projects" || path === "/api/history/targets") return { items: [] };
      if (path === `/api/history/${entry.id}`) return entry;
      return { items: [entry], total: 1, limit: 50, has_more: false };
    });
    render(<HistoryPage />);
    const target = await screen.findByRole("button", { name: "Open history details for unknown target" });
    expect(within(target).getByText("-")).toBeVisible();
    await userEvent.click(target);
    expect(await screen.findByRole("dialog")).toBeVisible();
    expect(apiGet).toHaveBeenCalledWith(`/api/history/${entry.id}`, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });

  it("reports invalid list metadata instead of rendering an unverified entry", async () => {
    installHistoryMock();
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      return Promise.resolve({ ...historyResponse("unsafe"), items: [{ id: "unsafe" }] });
    });
    render(<HistoryPage />);
    expect(await screen.findByText("Invalid history response from gateway.")).toBeVisible();
    expect(screen.queryByText("unsafe")).not.toBeInTheDocument();
  });

  it("keeps refreshed target facets when an older reference request completes late", async () => {
    const older = deferred();
    let targetReads = 0;
    installHistoryMock();
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/projects") return Promise.resolve({ items: [] });
      if (path === "/api/history/targets") {
        targetReads++;
        return targetReads === 1
          ? older.promise
          : Promise.resolve({
              items: [
                {
                  ref: "fixture:2:2",
                  connector_kind: "fixture",
                  target_id: 2,
                  profile_id: 2,
                  target_name: "Current target",
                  last_seen_at: "2026-09-26",
                },
              ],
            });
      }
      return Promise.resolve(historyResponse("initial"));
    });
    render(<HistoryPage />);
    expect(await screen.findByText("initial")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(await screen.findByRole("option", { name: "Current target / default" })).toBeInTheDocument();
    await act(async () =>
      older.resolve({
        items: [
          {
            ref: "fixture:1:1",
            connector_kind: "fixture",
            target_id: 1,
            profile_id: 1,
            target_name: "Older target",
            last_seen_at: "2026-09-25",
          },
        ],
      }),
    );
    expect(screen.queryByRole("option", { name: "Older target / default" })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Current target / default" })).toBeInTheDocument();
  });
});

describe("HistoryPage request ownership", () => {
  beforeEach(() => {
    apiGet.mockReset();
  });

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
    const total = screen.getByText("Total").parentElement;
    if (!total) throw new Error("Missing total stat");
    expect(within(total).getByText("7")).toBeVisible();
  });

  it("does not describe a failed history request as an empty history", async () => {
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (typeof path === "string" && path.startsWith("/api/history?")) return Promise.reject(new Error("history unavailable"));
      return Promise.resolve({});
    });
    render(<HistoryPage />);

    expect(await screen.findByText("history unavailable")).toBeVisible();
    expect(screen.queryByText("No history yet.")).not.toBeInTheDocument();
  });

  it("clears stale forward pagination after the next page fails", async () => {
    let historyCalls = 0;
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (typeof path !== "string" || !path.startsWith("/api/history?")) return Promise.resolve({});
      historyCalls++;
      if (historyCalls === 1) return Promise.resolve(historyResponse("page one", { nextCursor: "page-2" }));
      return Promise.reject(new Error("next page unavailable"));
    });
    render(<HistoryPage />);

    expect(await screen.findByText("page one")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("next page unavailable")).toBeVisible();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Previous" })).toBeEnabled();
  });

  it("keeps the history table horizontally recoverable", async () => {
    installHistoryMock();
    render(<HistoryPage />);
    expect(await screen.findByText("initial")).toBeVisible();
    expect(screen.getByTestId("history-table-scroll")).toHaveClass("overflow-x-auto");
  });

  it("updates and clears every connector-aware history filter", async () => {
    const user = userEvent.setup();
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([{ id: 5, name: "Investigate", color: "#ffffff" }]);
      if (path === "/api/projects") return Promise.resolve({ items: [{ id: 3, name: "My Project", slug: "my-project", target_count: 1 }] });
      if (path === "/api/history/targets") {
        return Promise.resolve({
          items: [
            {
              ref: "ssh:1:1",
              connector_kind: "ssh",
              target_name: "Host",
              project_id: 3,
              target_id: 1,
              profile_id: 1,
              last_seen_at: "2026-09-26",
            },
          ],
        });
      }
      if (typeof path === "string" && path.startsWith("/api/history?")) return Promise.resolve(historyResponse("initial"));
      return Promise.resolve({});
    });
    render(<HistoryPage />);
    expect(await screen.findByText("initial")).toBeVisible();

    await user.selectOptions(screen.getByLabelText("Filter by project"), "3");
    await user.selectOptions(screen.getByLabelText("Filter by connector type"), "ssh");
    await user.selectOptions(screen.getByLabelText("Filter by status"), "completed");
    await user.selectOptions(screen.getByLabelText("Filter by source"), "mcp");
    await user.selectOptions(screen.getByLabelText("Filter by connector"), "ssh:1:1");
    await user.selectOptions(screen.getByLabelText("Filter by label"), "5");
    await user.type(screen.getByLabelText("Search history"), "query");
    await user.click(screen.getByRole("button", { name: "Clear filters" }));

    expect(screen.getByLabelText("Search history")).toHaveValue("");
    expect(screen.getByLabelText("Filter by project")).toHaveValue("");
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
      if (path === "/api/history/1") return detail.promise;
      if (typeof path === "string" && path.startsWith("/api/history?")) return Promise.resolve(historyResponse("initial"));
      return Promise.resolve({});
    });
    render(<HistoryPage />);

    fireEvent.click(await screen.findByText("initial"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close dialog" }));
    await act(async () => detail.resolve({ ...historyResponse("detail").items[0], id: 1 }));

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("opens history details from the keyboard", async () => {
    const user = userEvent.setup();
    installHistoryMock();
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (path === "/api/history/1") return Promise.resolve(historyResponse("initial").items[0]);
      if (typeof path === "string" && path.startsWith("/api/history?")) return Promise.resolve(historyResponse("initial"));
      return Promise.resolve({});
    });
    render(<HistoryPage />);

    const details = await screen.findByRole("button", { name: "Open history details for initial" });
    details.focus();
    await user.keyboard("{Enter}");

    expect(await screen.findByRole("dialog")).toBeVisible();
  });

  it("opens history details from the whole row", async () => {
    installHistoryMock();
    apiGet.mockImplementation((path) => {
      if (path === "/api/history-labels") return Promise.resolve([]);
      if (path === "/api/history/targets" || path === "/api/projects") return Promise.resolve({ items: [] });
      if (path === "/api/history/1") return Promise.resolve(historyResponse("initial").items[0]);
      if (typeof path === "string" && path.startsWith("/api/history?")) return Promise.resolve(historyResponse("initial"));
      return Promise.resolve({});
    });
    render(<HistoryPage />);

    const row = (await screen.findByText("initial")).closest("tr");
    if (!row) throw new Error("Missing history row");
    fireEvent.click(row.cells[0]);

    expect(await screen.findByRole("dialog")).toBeVisible();
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
