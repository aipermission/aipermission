import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useRabbitMQPublishOwnership } from "./use-rabbitmq-publish-ownership";

const noActiveRequests = async () => [];

it("allows only the current publish attempt to update or release ownership", async () => {
  const { result } = renderHook(() => useRabbitMQPublishOwnership("rabbitmq:1:1", [], "ready", noActiveRequests));

  let attemptID;
  act(() => {
    attemptID = result.current.begin();
  });
  expect(result.current.locked).toBe(true);

  act(() => {
    expect(result.current.update(attemptID + 1, { requestID: 700, observed: true })).toBe(false);
    expect(result.current.release(attemptID + 1)).toBe(false);
  });
  expect(result.current.ownerRef.current).toMatchObject({ attemptID, requestID: null, observed: false });

  act(() => {
    expect(result.current.update(attemptID, { requestID: 700, observed: false })).toBe(true);
  });
  expect(result.current.ownerRef.current).toMatchObject({ attemptID, requestID: 700, observed: false });

  act(() => {
    expect(result.current.release(attemptID)).toBe(true);
  });
  await waitFor(() => expect(result.current.locked).toBe(false));
});

it("releases local ownership when the connector scope changes", async () => {
  const { result, rerender } = renderHook(({ scopeKey }) => useRabbitMQPublishOwnership(scopeKey, [], "ready", noActiveRequests), {
    initialProps: { scopeKey: "rabbitmq:1:1" },
  });
  act(() => result.current.begin());
  expect(result.current.locked).toBe(true);

  rerender({ scopeKey: "rabbitmq:2:2" });
  await waitFor(() => expect(result.current.locked).toBe(false));
});

it("does not adopt a visible pending publish until approval state is ready", () => {
  const pending = { id: 504, action_name: "publish_message", status: "approval_pending" };
  const { result, rerender } = renderHook(({ approvalState }) => useRabbitMQPublishOwnership("rabbitmq:1:1", [pending], approvalState), {
    initialProps: { approvalState: "loading" },
  });
  expect(result.current.ownerRef.current).toBeNull();

  rerender({ approvalState: "ready" });
  expect(result.current.ownerRef.current).toMatchObject({ requestID: 504, observed: true });
});

it("recovers unresolved publish ownership outside the bounded activity feed", async () => {
  const pending = { id: 505, action_name: "publish_message", status: "approval_pending" };
  const getAction = vi.fn().mockResolvedValue([pending]);
  const { result } = renderHook(() => useRabbitMQPublishOwnership("rabbitmq:4:8", [], "ready", getAction));

  await waitFor(() => expect(result.current.ownerRef.current).toMatchObject({ requestID: 505, observed: true }));
  expect(getAction).toHaveBeenCalledWith(
    "/api/connector-action-approvals?target_ref=rabbitmq%3A4%3A8&action_name=publish_message&active=true",
    expect.any(Object),
  );
  expect(result.current.locked).toBe(true);
});

it("fails closed and retries when scoped ownership discovery is unavailable", async () => {
  vi.useFakeTimers();
  const getAction = vi.fn().mockRejectedValueOnce(new Error("temporary lookup failure")).mockResolvedValueOnce([]);
  const rendered = renderHook(() => useRabbitMQPublishOwnership("rabbitmq:4:9", [], "ready", getAction));
  try {
    await act(async () => {});
    expect(getAction).toHaveBeenCalledOnce();
    expect(rendered.result.current.locked).toBe(true);

    await act(async () => vi.advanceTimersByTimeAsync(3000));
    expect(getAction).toHaveBeenCalledTimes(2);
    expect(rendered.result.current.locked).toBe(false);
  } finally {
    rendered.unmount();
    vi.useRealTimers();
  }
});

it("uses the exact request endpoint when a pending publish falls outside the bounded activity list", async () => {
  let resolveExact;
  const getAction = vi.fn(
    () =>
      new Promise((resolve) => {
        resolveExact = resolve;
      }),
  );
  const pending = { id: 501, action_name: "publish_message", status: "approval_pending" };
  const { result, rerender } = renderHook(({ items }) => useRabbitMQPublishOwnership("rabbitmq:1:1", items, "ready", getAction), {
    initialProps: { items: [pending] },
  });
  await waitFor(() => expect(result.current.locked).toBe(true));

  rerender({ items: [] });
  await waitFor(() => expect(getAction).toHaveBeenCalledWith("/api/connector-action-approvals/501", expect.any(Object)));
  expect(result.current.locked).toBe(true);
  await act(async () => resolveExact(pending));
  expect(result.current.locked).toBe(true);
});

it("releases exact request ownership only after an explicit terminal response", async () => {
  const getAction = vi.fn().mockResolvedValue({ id: 502, action_name: "publish_message", status: "failed" });
  const pending = { id: 502, action_name: "publish_message", status: "outcome_unknown" };
  const { result, rerender } = renderHook(({ items }) => useRabbitMQPublishOwnership("rabbitmq:1:1", items, "ready", getAction), {
    initialProps: { items: [pending] },
  });
  await waitFor(() => expect(result.current.locked).toBe(true));
  rerender({ items: [] });
  await waitFor(() => expect(result.current.locked).toBe(false));
});

it("keeps ownership when the exact request lookup fails", async () => {
  const getAction = vi.fn().mockRejectedValue(new Error("temporary lookup failure"));
  const pending = { id: 503, action_name: "publish_message", status: "approval_pending" };
  const { result, rerender } = renderHook(({ items }) => useRabbitMQPublishOwnership("rabbitmq:1:1", items, "ready", getAction), {
    initialProps: { items: [pending] },
  });
  await waitFor(() => expect(result.current.locked).toBe(true));

  rerender({ items: [] });
  await waitFor(() => expect(getAction).toHaveBeenCalledOnce());
  expect(result.current.locked).toBe(true);
});
