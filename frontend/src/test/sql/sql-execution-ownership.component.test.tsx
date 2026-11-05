import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { useSQLConsole } from "../../connectors/templates/_shared/use-sql-console";
import { listLocalActionRetryEntries } from "../../lib/local-action-retry";
import { mutationObservationQueue } from "../connector-mutation-test-state";
import { executionApproval, executionProps, executionResponse, setupSQLExecutionTransport } from "./execution-test-state";
import type { SQLConsoleProps } from "../../connectors/templates/_shared/use-sql-console";

vi.mock("../../connectors/templates/_shared/observation-scheduler", () => ({
  scheduleObservation: (observe: () => Promise<void>, delay: number) => observations.schedule(observe, delay),
}));
const observations = mutationObservationQueue();
const transport = setupSQLExecutionTransport();
beforeEach(() => observations.clear());

function hook(overrides: Partial<SQLConsoleProps> = {}) {
  return renderHook((props: SQLConsoleProps) => useSQLConsole(props), { initialProps: { ...executionProps, ...overrides } });
}
async function ready(rendered: ReturnType<typeof hook>) {
  await waitFor(() => expect(rendered.result.current.executionLocked).toBe(false));
  act(() => {
    rendered.result.current.setExecutionMode(true);
    rendered.result.current.setSQL("SELECT 1");
  });
}
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

it("blocks execution while approval state is unavailable and while discovery is unresolved", async () => {
  const discovery = deferred<unknown>();
  transport.discover.mockReturnValue(discovery.promise);
  const rendered = hook({ approvals: { state: "loading", data: [] } });
  act(() => {
    rendered.result.current.setExecutionMode(true);
    rendered.result.current.setSQL("SELECT 1");
  });
  await act(async () => rendered.result.current.runQuery());
  expect(transport.execute).not.toHaveBeenCalled();
  expect(transport.discover).not.toHaveBeenCalled();
  rendered.rerender(executionProps);
  await act(async () => rendered.result.current.runQuery());
  expect(transport.execute).not.toHaveBeenCalled();
  await act(async () => discovery.resolve([]));
  await waitFor(() => expect(rendered.result.current.executionLocked).toBe(false));
});

it("fails closed on discovery errors without dispatching a script", async () => {
  transport.discover.mockRejectedValue(new Error("offline"));
  const rendered = hook();
  await waitFor(() => expect(observations.pending).toBeGreaterThan(0));
  act(() => {
    rendered.result.current.setExecutionMode(true);
    rendered.result.current.setSQL("SELECT 1");
  });
  await act(async () => rendered.result.current.runQuery());
  expect(rendered.result.current.executionLocked).toBe(true);
  expect(transport.execute).not.toHaveBeenCalled();
});

it("admits only one execution through repeated keyboard submits before any render", async () => {
  const pending = deferred<unknown>();
  transport.execute.mockReturnValue(pending.promise);
  const rendered = hook();
  await ready(rendered);
  const run = rendered.result.current.runQuery;
  let first!: Promise<void>;
  await act(async () => {
    first = run();
    await run();
  });
  await waitFor(() => expect(transport.execute).toHaveBeenCalledOnce());
  await observations.advance();
  expect(rendered.result.current.executionLocked).toBe(true);
  await act(async () => {
    pending.resolve(executionResponse());
    await first;
  });
  expect(rendered.result.current.selectedID).toBe(81);
  expect(transport.execute).toHaveBeenCalledOnce();
});

it.each(["running", "approval_pending", "outcome_unknown"] as const)(
  "retains %s request identity across draft changes, refresh failure and remount with no script retry",
  async (status) => {
    transport.execute.mockResolvedValue(
      executionResponse({ status, error: status === "outcome_unknown" ? "Outcome unknown; inspect the server." : undefined }),
    );
    const onRefreshActivity = vi.fn().mockResolvedValue(undefined);
    const first = hook({ onRefreshActivity });
    await ready(first);
    await waitFor(() => expect(onRefreshActivity).toHaveBeenCalled());
    onRefreshActivity.mockRejectedValue(new Error("activity offline"));
    await act(async () => first.result.current.runQuery());
    expect(first.result.current.executionLocked).toBe(true);
    expect(first.result.current.executionStatus).toContain("#81");
    expect(first.result.current.executionStatus).toContain("Settings > Unresolved server requests");
    if (status === "outcome_unknown") expect(first.result.current.runState.error).toContain("Request 81");
    else expect(first.result.current.selectedID).toBe(81);
    act(() => first.result.current.setSQL("DELETE FROM users"));
    await act(async () => first.result.current.runQuery());
    expect(transport.execute).toHaveBeenCalledOnce();
    expect(await listLocalActionRetryEntries()).toEqual([
      expect.objectContaining({
        request_id: 81,
        action_name: "execute_sql",
        mutation_guard: true,
        state: status === "outcome_unknown" ? "outcome_unknown" : "pending",
      }),
    ]);
    first.unmount();
    transport.discover.mockImplementation(async (url) =>
      url.endsWith("/81") ? executionApproval({ status }) : [executionApproval({ status })],
    );
    const next = hook();
    await waitFor(() => expect(next.result.current.executionStatus).toContain("#81"));
    expect(next.result.current.executionMode).toBe(false);
    act(() => {
      next.result.current.setExecutionMode(true);
      next.result.current.setSQL("SELECT 2");
    });
    await act(async () => next.result.current.runQuery());
    expect(transport.execute).toHaveBeenCalledOnce();
    expect(next.result.current.executionLocked).toBe(true);
  },
);

it("keeps the durable exclusive ledger after a lost reply and discovers its request on reload", async () => {
  transport.execute.mockRejectedValue(new TypeError("reply lost after dispatch"));
  const first = hook();
  await ready(first);
  await act(async () => first.result.current.runQuery());
  expect(first.result.current.executionLocked).toBe(true);
  await observations.advance();
  expect(first.result.current.executionLocked).toBe(true);
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ mutation_guard: true, state: "pending" })]);
  await act(async () => first.result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledOnce();
  first.unmount();
  transport.discover.mockResolvedValue([executionApproval({ status: "outcome_unknown" })]);
  const next = hook();
  await waitFor(() => expect(next.result.current.executionStatus).toContain("#81"));
  act(() => {
    next.result.current.setExecutionMode(true);
    next.result.current.setSQL("SELECT 2");
  });
  await act(async () => next.result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledOnce();
});

it("keeps observed unknown outcomes owned even when discovery no longer lists the request", async () => {
  transport.execute.mockResolvedValue(executionResponse({ status: "outcome_unknown", error: "Unknown result" }));
  const rendered = hook();
  await ready(rendered);
  await act(async () => rendered.result.current.runQuery());
  transport.discover.mockImplementation(async (url) => (url.endsWith("/81") ? executionApproval({ status: "completed" }) : []));
  await observations.advance();
  expect(rendered.result.current.executionLocked).toBe(true);
  expect(rendered.result.current.executionStatus).toContain("#81");
  await act(async () => rendered.result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledOnce();
});

it("does not mistake a pending refresh error for permission to rerun and releases only on terminal observation", async () => {
  transport.execute.mockResolvedValue(executionResponse({ status: "approval_pending" }));
  const rendered = hook({
    onRefreshActivity: () => {
      throw new Error("refresh offline");
    },
  });
  await ready(rendered);
  await act(async () => rendered.result.current.runQuery());
  expect(rendered.result.current.runState.error).toContain("Approval is pending");
  transport.discover.mockRejectedValue(new Error("observation offline"));
  await observations.advance();
  expect(rendered.result.current.executionLocked).toBe(true);
  expect(rendered.result.current.executionStatus).toContain("#81");
  await act(async () => rendered.result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledOnce();
  transport.discover.mockImplementation(async (url) => (url.endsWith("/81") ? executionApproval({ status: "completed" }) : []));
  await observations.advance();
  await waitFor(() => expect(rendered.result.current.executionLocked).toBe(false));
  expect(await listLocalActionRetryEntries()).toEqual([]);
  expect(transport.execute).toHaveBeenCalledOnce();
});

it.each(["target", "profile", "session"])(
  "ignores a midflight execution reply and stale submit handler after %s switch",
  async (change) => {
    const pending = deferred<unknown>();
    transport.execute.mockReturnValue(pending.promise);
    const rendered = hook();
    await ready(rendered);
    const run = rendered.result.current.runQuery;
    let running!: Promise<void>;
    act(() => {
      running = run();
    });
    await waitFor(() => expect(transport.execute).toHaveBeenCalledOnce());
    const signal: AbortSignal = transport.posts.mock.calls.find(([body]) => body.action_name === "execute_sql")?.[1].signal;
    const next = { ...executionProps };
    if (change === "target") next.target = { ref: "sql-test:2:1", name: "Other" };
    if (change === "profile") next.target = { ref: "sql-test:1:2", name: "Other profile" };
    if (change === "session") next.session = { active: true, startedAt: "2026-01-02T12:00:00Z" };
    rendered.rerender(next);
    expect(signal.aborted).toBe(true);
    expect(rendered.result.current.executionMode).toBe(false);
    await act(async () => run());
    expect(transport.execute).toHaveBeenCalledOnce();
    await act(async () => {
      pending.resolve(executionResponse({ status: "outcome_unknown" }));
      await running;
    });
    expect(rendered.result.current.selectedID).toBeNull();
    expect(rendered.result.current.runState).toEqual({ state: "idle", error: "" });
    transport.discover.mockImplementation(async (url) =>
      url.includes("sql-test%3A1%3A1") ? [executionApproval({ status: "outcome_unknown" })] : [],
    );
    rendered.rerender(executionProps);
    if (change === "session") await observations.advance();
    await waitFor(() => expect(rendered.result.current.executionStatus).toContain("#81"));
    expect(rendered.result.current.executionMode).toBe(false);
    act(() => rendered.result.current.setExecutionMode(true));
    await act(async () => rendered.result.current.runQuery());
    expect(transport.execute).toHaveBeenCalledOnce();
  },
);
