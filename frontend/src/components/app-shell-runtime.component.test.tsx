import { describe, expect, it, vi } from "vitest";

vi.mock("../lib/api.js", () => ({
  apiUrl: "https://localhost:3210",
  currentWorkspaceBinding: vi.fn(() => "workspace/a"),
}));

import {
  consoleSessionAttachUrl,
  createPollGenerationGuard,
  isActiveTransferBatch,
  limitTranscript,
  liveConsoleRuntimeTargets,
  mergeConsoleSessionData,
  normalizeCredentialResources,
  parseConsoleSocketMessage,
} from "./app-shell-runtime";

describe("app shell runtime", () => {
  it("binds secure console sockets to the encoded workspace identity", () => {
    expect(consoleSessionAttachUrl(7)).toBe("wss://localhost:3210/api/console/sessions/7/attach?workspace=workspace%2Fa");
  });

  it("keeps only connector targets with a live runtime projection", () => {
    type Target = { connector_kind: string; runtime_id?: number; enabled: boolean };
    const models: Record<
      string,
      {
        usesLiveConsole: (_options: { target: Target }) => boolean;
        liveConsoleRuntimeTarget?: (_options: { target: Target }) => { id: number | undefined };
      }
    > = {
      live: {
        usesLiveConsole: ({ target }) => target.enabled,
        liveConsoleRuntimeTarget: ({ target }) => ({ id: target.runtime_id }),
      },
      static: { usesLiveConsole: () => false },
    };

    expect(
      liveConsoleRuntimeTargets(
        [
          { connector_kind: "live", runtime_id: 11, enabled: true },
          { connector_kind: "live", runtime_id: 12, enabled: false },
          { connector_kind: "static", runtime_id: 13, enabled: true },
          { connector_kind: "live", enabled: true },
        ],
        (kind) => models[kind],
      ),
    ).toEqual([{ id: 11 }]);
    expect(liveConsoleRuntimeTargets(null, () => null)).toEqual([]);
  });

  it("resolves each connector model once without assuming runtime identity is a profile ID", () => {
    const getModel = vi.fn(() => ({
      usesLiveConsole: () => true,
      liveConsoleRuntimeTarget: ({ target }: { target: { runtime_id?: number; profile_id: number } }) => ({
        id: target.runtime_id,
        profile: target.profile_id,
      }),
    }));
    expect(liveConsoleRuntimeTargets([{ connector_kind: "runtime", runtime_id: 91, profile_id: 2 }], getModel)).toEqual([
      { id: 91, profile: 2 },
    ]);
    expect(getModel).toHaveBeenCalledExactlyOnceWith("runtime");
  });

  it("retires poll generations without rejecting uncaptured polls", () => {
    const guard = createPollGenerationGuard();
    const first = guard.begin();
    expect(guard.isCurrent(first)).toBe(true);
    const second = guard.begin();
    expect(guard.isCurrent(first)).toBe(false);
    expect(guard.isCurrent(second)).toBe(true);
    guard.invalidate();
    expect(guard.isCurrent(second)).toBe(false);
    expect(guard.isCurrent()).toBe(true);
  });

  it("normalizes resource identities without dropping existing connector metadata", () => {
    const existing = { id: 7, connector_kind: "native", resource_kind: "key", resource_ref: "native:key:7" };
    expect(normalizeCredentialResources("generic", [existing])).toEqual([existing]);
    expect(normalizeCredentialResources("generic", [{ id: 8, kind: "profile" }, { name: "default" }, {}])).toEqual([
      { id: 8, kind: "profile", connector_kind: "generic", resource_kind: "profile", resource_ref: "generic:profile:8" },
      { name: "default", connector_kind: "generic", resource_kind: "credential", resource_ref: "generic:credential:default" },
      { connector_kind: "generic", resource_kind: "credential", resource_ref: "generic:credential:unknown" },
    ]);
    expect(normalizeCredentialResources("generic", null)).toEqual([]);
  });

  it.each([null, {}, "not-json", "null", "[]", "{}", '{"type":2}'])("rejects malformed socket messages %j", (input) => {
    expect(parseConsoleSocketMessage(input)).toBeNull();
  });

  it("preserves typed socket events and bounds local transcripts", () => {
    expect(parseConsoleSocketMessage('{"type":"output","text":"hello"}')).toEqual({ type: "output", text: "hello" });
    expect(limitTranscript("small")).toBe("small");
    expect(limitTranscript(`discard${"x".repeat(200000)}`)).toBe("x".repeat(200000));
    expect(isActiveTransferBatch({ status: "paused" })).toBe(true);
    expect(isActiveTransferBatch({ status: "completed" })).toBe(false);
    expect(isActiveTransferBatch(null)).toBe(false);
  });

  it("preserves attached transcript state only for sessions that remain live", () => {
    type SessionFixture = { id: string | number; status: string; transcript: string; error: string | null };
    const current: SessionFixture[] = [
      { id: 1, status: "connected", transcript: "local stream", error: null },
      { id: 2, status: "closed", transcript: "old stream", error: "old error" },
    ];
    const next: SessionFixture[] = [
      { id: "1", status: "connecting", transcript: "stale poll", error: null },
      { id: "2", status: "connected", transcript: "new stream", error: null },
      { id: "3", status: "connected", transcript: "first stream", error: null },
    ];
    expect(mergeConsoleSessionData(next, current)).toEqual([
      { ...next[0], status: "connected", transcript: "local stream" },
      next[1],
      next[2],
    ]);
    expect(mergeConsoleSessionData([{ ...next[0], status: "closed" }], current)).toEqual([{ ...next[0], status: "closed" }]);
  });
});
