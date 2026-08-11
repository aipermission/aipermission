import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useConsolePageState } from "./use-console-page-state";

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
          messages: { data: [{ id: 1, runtime_id: 11, direction: "ai_to_user", consumed_at: null }] },
          sessions: [{ id: 1, runtime_id: 22, status: "connected", transcript: "second session" }],
        },
      },
    );

    expect(result.current.selectedRuntimeTarget.id).toBe(22);
    expect(result.current.selectedSession.transcript).toBe("second session");
    expect(result.current.defaultRuntimeID).toBe("11");

    rerender({
      messages: { data: [{ id: 2, runtime_id: 22, direction: "ai_to_user", consumed_at: null }] },
      sessions: [
        { id: 2, runtime_id: 11, status: "connected", transcript: "new first session" },
        { id: 1, runtime_id: 22, status: "connected", transcript: "updated second session" },
      ],
    });

    expect(result.current.selectedRuntimeTarget.id).toBe(22);
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
});
