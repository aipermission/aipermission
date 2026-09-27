import { expect, expectTypeOf, it } from "vitest";
import { capturedConsoleSlots, defineConsoleTemplate } from "./console-template";

it("preserves native inference while exposing only shared console slots", () => {
  const Console = () => null;
  const ToolbarActions = () => null;
  const Form = (_props: { username: string }) => null;
  const native = defineConsoleTemplate({ Console, ToolbarActions, Form, model: { emptyForm: () => ({ username: "" }) } });
  expectTypeOf(native.Form).parameter(0).toEqualTypeOf<{ username: string }>();
  expectTypeOf(native.model.emptyForm).returns.toEqualTypeOf<{ username: string }>();
  expect(native.Form).toBe(Form);
  expect(Object.isFrozen(native)).toBe(true);
  const slots = capturedConsoleSlots(native);
  expect(slots).toEqual({ Console, ToolbarActions });
  expect(Object.isFrozen(slots)).toBe(true);
  expect(slots).not.toHaveProperty("Form");
  expect(slots).not.toHaveProperty("model");
});

it("does not accept copies or structural lookalikes as captured slots", () => {
  const native = defineConsoleTemplate({ Console: () => null });
  expect(capturedConsoleSlots(native)).not.toHaveProperty("ToolbarActions");
  for (const value of [null, undefined, {}, [], () => {}, { ...native }]) expect(capturedConsoleSlots(value)).toBeNull();
});
