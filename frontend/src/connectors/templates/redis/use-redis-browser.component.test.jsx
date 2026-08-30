import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useRedisBrowser } from "./use-redis-browser";

function renderBrowser() {
  return renderHook(
    ({ target }) =>
      useRedisBrowser({
        target,
        approvals: { data: [] },
        session: { active: false, startedAt: "" },
        onRefreshActivity: vi.fn(),
      }),
    { initialProps: { target: { ref: "redis:1:1", connector_kind: "redis", config: {} } } },
  );
}

describe("useRedisBrowser", () => {
  it("drops pending writes and stale status when the target changes", () => {
    const hook = renderBrowser();
    act(() => {
      hook.result.current.setNewKey("session:key");
      hook.result.current.setNewValue("value");
    });
    act(() => hook.result.current.saveStringValue());
    expect(hook.result.current.confirmDialog.open).toBe(true);

    hook.rerender({ target: { ref: "redis:2:2", connector_kind: "redis", config: {} } });

    expect(hook.result.current.confirmDialog.open).toBe(false);
    expect(hook.result.current.state).toEqual({ state: "idle", error: "", message: "" });
    expect(hook.result.current.newKey).toBe("");
  });
});
