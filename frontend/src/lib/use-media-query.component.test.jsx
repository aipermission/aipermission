import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useMediaQuery } from "./use-media-query";

afterEach(() => {
  vi.unstubAllGlobals();
});

it("tracks media query changes and removes its listener", () => {
  let matches = false;
  let listener;
  const removeEventListener = vi.fn();
  vi.stubGlobal("matchMedia", () => ({
    get matches() {
      return matches;
    },
    addEventListener: vi.fn((event, callback) => {
      expect(event).toBe("change");
      listener = callback;
    }),
    removeEventListener,
  }));

  const view = renderHook(() => useMediaQuery("(min-width: 1000px)"));
  expect(view.result.current).toBe(false);
  matches = true;
  act(() => listener());
  expect(view.result.current).toBe(true);
  view.unmount();
  expect(removeEventListener).toHaveBeenCalledWith("change", listener);
});

it("stays false when matchMedia is unavailable", () => {
  vi.stubGlobal("matchMedia", undefined);
  const { result } = renderHook(() => useMediaQuery("print"));
  expect(result.current).toBe(false);
});
