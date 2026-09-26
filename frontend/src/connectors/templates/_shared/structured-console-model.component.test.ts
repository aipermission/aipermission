import { expect, it } from "vitest";
import { createStructuredConsoleModel } from "./structured-console-model";

it("uses connector-provided profile defaults without adding connector-specific behavior", () => {
  const model = createStructuredConsoleModel("monitor");
  expect(model.targetProfileLabel({})).toBe("monitor");
  expect(model.targetProfileLabel({ target: null })).toBe("monitor");
  expect(model.targetProfileLabel({ target: {} })).toBe("monitor");
  expect(model.targetProfileLabel({ target: { profile_label: "" } })).toBe("monitor");
  expect(model.targetProfileLabel({ target: { profile_label: "reader" } })).toBe("reader");
  expect(createStructuredConsoleModel("readonly").targetProfileLabel({})).toBe("readonly");
  expect(model.usesLiveConsole()).toBe(false);
  const recoverable = model.recoverableRunningActions();
  recoverable.push("must not leak between calls");
  expect(model.recoverableRunningActions()).toEqual([]);
});
