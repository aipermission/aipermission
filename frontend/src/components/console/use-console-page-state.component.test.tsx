import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useConsolePageState } from "./use-console-page-state";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts.ts";

const targets = {
  data: [
    { id: 11, name: "first" },
    { id: 22, name: "second" },
  ],
};

describe("useConsolePageState", () => {
  it("keeps explicit selection stable while runtime data and unread messages change", () => {
    const { result, rerender } = renderHook(
      ({ messages, sessions }) =>
        useConsolePageState({ liveConsoleTargets: targets, messages, sessions, selectedRuntimeID: "22", allowTargetFallback: false }),
      {
        initialProps: {
          messages: { data: [message(1, 11)] },
          sessions: [{ id: 1, runtime_id: 22, status: "connected", transcript: "second session" }],
        },
      },
    );

    expect(result.current.selectedRuntimeTarget?.id).toBe(22);
    expect(result.current.selectedSession.transcript).toBe("second session");
    expect(result.current.defaultRuntimeID).toBe("11");

    rerender({
      messages: { data: [message(2, 22)] },
      sessions: [
        { id: 2, runtime_id: 11, status: "connected", transcript: "new first session" },
        { id: 1, runtime_id: 22, status: "connected", transcript: "updated second session" },
      ],
    });

    expect(result.current.selectedRuntimeTarget?.id).toBe(22);
    expect(result.current.selectedSession.transcript).toBe("updated second session");
    expect(result.current.selectedUnreadMessages).toHaveLength(1);
  });

  it("does not flash a fallback target while an explicit target is unavailable", () => {
    const { result } = renderHook(() =>
      useConsolePageState({
        liveConsoleTargets: targets,
        messages: { data: [] },
        sessions: [],
        selectedRuntimeID: "missing",
        allowTargetFallback: false,
      }),
    );

    expect(result.current.selectedRuntimeTarget).toBeNull();
    expect(result.current.selectedSession.status).toBe("idle");
  });

  it("does not choose any runtime or unread default when no targets are available", () => {
    const { result } = renderHook(() => useConsolePageState({
      liveConsoleTargets: { data: [] }, messages: { data: [message(1, 11)] }, sessions: [], selectedRuntimeID: "",
    }));
    expect(result.current.selectedRuntimeTarget).toBeNull();
    expect(result.current.selectedUnreadMessages).toEqual([]);
    expect(result.current.defaultRuntimeID).toBe("");
  });

  it("uses the first available target when fallback is allowed and ignores unread notes for missing runtimes", () => {
    const { result } = renderHook(() => useConsolePageState({
      liveConsoleTargets: targets, messages: { data: [message(1, 99)] }, sessions: [], selectedRuntimeID: "missing",
    }));
    expect(result.current.selectedRuntimeTarget?.id).toBe(11);
    expect(result.current.selectedSession.status).toBe("idle");
    expect(result.current.defaultRuntimeID).toBe("11");
  });

  it("respects an empty explicit selection when automatic target fallback is disabled", () => {
    const { result } = renderHook(() => useConsolePageState({
      liveConsoleTargets: targets, messages: { data: [] }, sessions: [], selectedRuntimeID: "", allowTargetFallback: false,
    }));
    expect(result.current.selectedRuntimeTarget).toBeNull();
  });
});

function message(id: number, runtimeID: number): RuntimeMessage {
  return { id, runtime_id: runtimeID, token_id: 1, direction: "ai_to_user", consumed_at: null, message: "A note", created_at: "2026-09-26" };
}
