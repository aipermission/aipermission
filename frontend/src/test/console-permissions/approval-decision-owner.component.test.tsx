import { renderHook } from "@testing-library/react";
import { expect, it } from "vitest";
import { createRequestGuard } from "../../lib/request-guard";
import { useApprovalDecisionOwner } from "../../lib/use-approval-decision-owner";

it("serializes owned decisions synchronously and permits only their owner to release them", () => {
  const requests = createRequestGuard("A");
  const { result } = renderHook(() => useApprovalDecisionOwner(requests, "decision"));
  expect(result.current.isPending()).toBe(false);
  const first = result.current.begin()!;
  expect(result.current.isPending()).toBe(true);
  expect(result.current.begin()).toBeNull();
  requests.setScope("B");
  expect(first.signal.aborted).toBe(true);
  expect(result.current.isPending()).toBe(false);
  const second = result.current.begin()!;
  result.current.complete(first);
  expect(result.current.isPending()).toBe(true);
  result.current.complete(second);
  expect(result.current.isPending()).toBe(false);
  const third = result.current.begin()!;
  requests.dispose();
  expect(result.current.isPending()).toBe(false);
  result.current.complete(third);
});
