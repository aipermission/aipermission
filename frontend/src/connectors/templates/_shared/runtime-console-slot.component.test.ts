import { expect, it } from "vitest";
import { gatewayTargetFixture } from "../../../test/connector-inventory-fixtures";
import { liveConsoleSlotSession, runtimeConsoleSlotTarget } from "./runtime-console-slot";

it("preserves independent target, profile and runtime identities without interpreting native runtime IDs", () => {
  const target = gatewayTargetFixture({
    target_id: 3,
    profile_id: 11,
    runtime_id: 91,
    ref: "example:3:11",
    config: { transport_target_ref: "transport:4:7", ignored: true },
  });
  expect(runtimeConsoleSlotTarget(target, "Example")).toEqual({
    target_id: 3,
    profile_id: 11,
    runtime_id: 91,
    ref: "example:3:11",
    config: { transport_target_ref: "transport:4:7" },
  });
  expect(runtimeConsoleSlotTarget({ ...target, config: undefined }, "Example").config.transport_target_ref).toBeUndefined();
  expect(() => runtimeConsoleSlotTarget({ ...target, config: { transport_target_ref: {} } }, "Example")).toThrow(
    "Invalid Example console target transport_target_ref.",
  );
});

it("projects live session identity and name without treating structured state as live", () => {
  expect(liveConsoleSlotSession({ id: 9, name: "console", transcript: "not a slot projection field" })).toEqual({ id: 9, name: "console" });
  expect(liveConsoleSlotSession({ id: 9 })).toEqual({ id: 9 });
  expect(liveConsoleSlotSession({ id: 9, name: undefined })).toEqual({ id: 9, name: undefined });
  for (const value of [
    null,
    [],
    {},
    false,
    { active: true, startedAt: "now" },
    { id: "9" },
    { id: 0 },
    { id: -1 },
    { id: 1.5 },
    { id: Infinity },
    { id: Number.MAX_SAFE_INTEGER + 1 },
    { id: 9, name: [] },
  ]) {
    expect(liveConsoleSlotSession(value)).toBeNull();
  }
});
