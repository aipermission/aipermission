import type { ConsoleTemplateContract } from "../console-template-contract";

type ConsoleSlots = Pick<ConsoleTemplateContract, "Console" | "ToolbarActions">;
const consoleSlots = new WeakMap<object, Readonly<ConsoleSlots>>();

export function defineConsoleTemplate<Template extends ConsoleTemplateContract>(template: Template): Readonly<Template> {
  const captured = Object.freeze({
    Console: template.Console,
    ...(template.ToolbarActions ? { ToolbarActions: template.ToolbarActions } : {}),
  });
  const native = Object.freeze(template);
  consoleSlots.set(native, captured);
  return native;
}

export function capturedConsoleSlots(value: unknown): Readonly<ConsoleSlots> | null {
  return value !== null && typeof value === "object" ? consoleSlots.get(value) || null : null;
}
