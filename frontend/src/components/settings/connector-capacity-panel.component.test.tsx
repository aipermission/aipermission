import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { connectorCapacityResponse, type ConnectorCapacityReport } from "../../lib/gateway-contracts/connector-capacity-contract";
import { invalidateUISession } from "../../lib/ui-session-events";
import { ConnectorCapacityPanel } from "./connector-capacity-panel";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

function report(): ConnectorCapacityReport {
  return {
    items: [
      {
        token_id: "9007199254740993",
        name: "My agent",
        rows: 1,
        stored_bytes: 1024,
        reserved_bytes: 0,
        running: 0,
        pending: 0,
        level: "ok",
      },
    ],
    row_limit: 20000,
    byte_limit: 268435456,
    running_limit: 4,
    next_request_reservation_bytes: 6291456,
    history_days: 2,
  };
}

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("connector capacity presentation and ownership", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("uses the authenticated read contract and shows configured limits without leaking extra fields", async () => {
    vi.mocked(apiGet).mockResolvedValue({ ...report(), token: "secret-extra" });
    render(<ConnectorCapacityPanel />);
    expect(await screen.findByText("My agent")).toBeVisible();
    expect(screen.getByText(/256\.0 MiB/)).toBeVisible();
    expect(screen.getByText(/History retention: 2 days/)).toBeVisible();
    expect(screen.getByRole("progressbar", { name: "Request storage for My agent" })).toHaveAttribute("value", "0.005");
    expect(apiGet).toHaveBeenCalledWith("/api/settings/connector-capacity", { signal: expect.any(AbortSignal), timeoutMs: 4000 });
    expect(document.body.textContent).not.toContain("secret-extra");
  });

  it("reports reserved bytes, exhausted storage and running saturation independently", async () => {
    const data = report();
    data.history_days = 0;
    data.items[0] = {
      ...data.items[0],
      rows: 5,
      running: 4,
      pending: 1,
      reserved_bytes: 5 * data.next_request_reservation_bytes,
      level: "exhausted",
    };
    vi.mocked(apiGet).mockResolvedValue(data);
    render(<ConnectorCapacityPanel />);
    expect(await screen.findByText("Storage full")).toBeVisible();
    expect(screen.getByText("30.0 MiB")).toBeVisible();
    expect(screen.getByText(/History retention is disabled/)).toBeVisible();
    expect(screen.getByText(/Waiting alone/)).toBeVisible();
    expect(screen.getByText(/Concurrent work limit/)).toBeVisible();
  });

  it.each(["warning", "critical"] as const)("uses backend %s classification", async (level) => {
    const data = report();
    data.items[0].level = level;
    vi.mocked(apiGet).mockResolvedValue(data);
    render(<ConnectorCapacityPanel />);
    expect(await screen.findByText(level === "warning" ? "80% used" : "90% used")).toBeVisible();
  });

  it("shows an empty token collection", async () => {
    vi.mocked(apiGet).mockResolvedValue({ ...report(), items: [] });
    render(<ConnectorCapacityPanel />);
    expect(await screen.findByText("No unrevoked tokens")).toBeVisible();
  });

  it("keeps refresh failures visible with an explicit stale reading and permits retry", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet)
      .mockResolvedValueOnce(report())
      .mockRejectedValueOnce(new Error("Usage unavailable"))
      .mockResolvedValueOnce({ ...report(), items: [] });
    render(<ConnectorCapacityPanel />);
    await screen.findByText("My agent");
    await user.click(screen.getByRole("button", { name: "Refresh connector capacity" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Usage unavailable.*Last successful reading/);
    expect(screen.getByText("My agent")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Refresh connector capacity" }));
    expect(await screen.findByText("No unrevoked tokens")).toBeVisible();
    expect(screen.queryByText(/Usage unavailable/)).not.toBeInTheDocument();
  });

  it("allows retry after an initial non-Error failure", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet).mockRejectedValueOnce(null).mockResolvedValueOnce(report());
    render(<ConnectorCapacityPanel />);
    expect(await screen.findByText("Could not load connector capacity.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Refresh connector capacity" }));
    expect(await screen.findByText("My agent")).toBeVisible();
  });

  it("aborts on unmount and ignores a late response", async () => {
    const pending = deferred();
    vi.mocked(apiGet).mockReturnValue(pending.promise);
    const { unmount } = render(<ConnectorCapacityPanel />);
    const signal = vi.mocked(apiGet).mock.calls[0][1]?.signal;
    unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () => pending.resolve(report()));
    expect(screen.queryByText("My agent")).not.toBeInTheDocument();
  });

  it("clears cached identities and rejects old replies when another client changes session", async () => {
    const user = userEvent.setup();
    const pending = deferred();
    vi.mocked(apiGet).mockResolvedValueOnce(report()).mockReturnValue(pending.promise);
    render(<ConnectorCapacityPanel />);
    await screen.findByText("My agent");
    await user.click(screen.getByRole("button", { name: "Refresh connector capacity" }));
    act(() => invalidateUISession());
    await act(async () => pending.resolve(report()));
    expect(screen.queryByText("My agent")).not.toBeInTheDocument();
    expect(screen.getByText(/Session changed/)).toBeVisible();
  });

  it("recovers refresh after the bounded read times out", async () => {
    vi.useFakeTimers();
    vi.mocked(apiGet)
      .mockImplementationOnce(
        () => new Promise((_resolve, reject) => window.setTimeout(() => reject(new Error("Gateway read timed out after 4000ms.")), 4000)),
      )
      .mockResolvedValueOnce(report());
    render(<ConnectorCapacityPanel />);
    await act(async () => vi.advanceTimersByTime(4000));
    expect(screen.getByRole("alert")).toHaveTextContent("Gateway read timed out");
    expect(screen.getByRole("button", { name: "Refresh connector capacity" })).toBeEnabled();
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(screen.getByText("My agent")).toBeVisible();
  });

  it("refreshes visible panels without overlapping pending reads and cleans up timers", async () => {
    vi.useFakeTimers();
    const pending = deferred();
    vi.mocked(apiGet).mockResolvedValueOnce(report()).mockReturnValue(pending.promise);
    const { unmount } = render(<ConnectorCapacityPanel />);
    await act(async () => {});
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    await act(async () => vi.advanceTimersByTime(30_000));
    expect(apiGet).toHaveBeenCalledTimes(1);
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
    await act(async () => vi.advanceTimersByTime(60_000));
    expect(apiGet).toHaveBeenCalledTimes(2);
    unmount();
    await act(async () => vi.advanceTimersByTime(60_000));
    expect(apiGet).toHaveBeenCalledTimes(2);
  });

  it("survives StrictMode teardown and accepts only the current response", async () => {
    const first = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(first.promise).mockResolvedValueOnce(report());
    render(
      <StrictMode>
        <ConnectorCapacityPanel />
      </StrictMode>,
    );
    expect(await screen.findByText("My agent")).toBeVisible();
    await act(async () => first.resolve({ ...report(), items: [] }));
    await waitFor(() => expect(screen.getByText("My agent")).toBeVisible());
  });
});

describe("capacity response validation", () => {
  it("preserves lossless identities and only allowlisted metadata", () => {
    expect(connectorCapacityResponse({ ...report(), password: "extra" })).toEqual(report());
  });

  it.each([
    null,
    [],
    {},
    { ...report(), byte_limit: 0 },
    { ...report(), row_limit: -1 },
    { ...report(), running_limit: Infinity },
    { ...report(), history_days: "2" },
    { ...report(), next_request_reservation_bytes: 0 },
  ])("rejects malformed limits %j", (value) => {
    expect(() => connectorCapacityResponse(value)).toThrow("capacity response is invalid");
  });

  it.each([
    { token_id: 9007199254740992 },
    { token_id: "9223372036854775808" },
    { token_id: "01" },
    { name: {} },
    { level: "unknown" },
    { level: ["exhausted"] },
    { rows: -1 },
    { stored_bytes: 2 ** 53 },
    { reserved_bytes: 1 },
    { running: 2 },
    { pending: "1" },
    { running: 1, reserved_bytes: 6291456, stored_bytes: Number.MAX_SAFE_INTEGER },
  ])("rejects malformed token metadata %j", (mutation) => {
    expect(() => connectorCapacityResponse({ ...report(), items: [{ ...report().items[0], ...mutation }] })).toThrow(
      "capacity response is invalid",
    );
  });

  it("rejects duplicated token identities", () => {
    expect(() => connectorCapacityResponse({ ...report(), items: [report().items[0], report().items[0]] })).toThrow();
  });
});
