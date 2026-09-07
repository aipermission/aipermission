import { describe, expect, it } from "vitest";
import { consoleShellGridClass } from "./console-layout";

describe("consoleShellGridClass", () => {
  it.each([
    [false, false, "grid-cols-[360px_minmax(0,1fr)_360px]"],
    [true, false, "grid-cols-[56px_minmax(0,1fr)_360px]"],
    [false, true, "grid-cols-[360px_minmax(0,1fr)_56px]"],
    [true, true, "grid-cols-[56px_minmax(0,1fr)_56px]"],
  ])("maps targetsCompact=%s and tokensCompact=%s", (targetsCompact, tokensCompact, expected) => {
    expect(consoleShellGridClass(targetsCompact, tokensCompact)).toBe(expected);
  });
});
