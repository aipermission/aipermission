import { describe, expect, it } from "vitest";
import { consoleShellGridClass } from "./console-layout";

describe("consoleShellGridClass", () => {
  it.each([
    [false, false, "2xl:grid-cols-[clamp(240px,20vw,360px)_minmax(0,1fr)_clamp(240px,20vw,360px)]"],
    [true, false, "2xl:grid-cols-[56px_minmax(0,1fr)_clamp(240px,20vw,360px)]"],
    [false, true, "2xl:grid-cols-[clamp(240px,20vw,360px)_minmax(0,1fr)_56px]"],
    [true, true, "2xl:grid-cols-[56px_minmax(0,1fr)_56px]"],
  ])("maps targetsCompact=%s and tokensCompact=%s", (targetsCompact, tokensCompact, expected) => {
    expect(consoleShellGridClass(targetsCompact, tokensCompact)).toBe(expected);
  });
});
