import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { readLocalPreference, removeLocalPreference, writeLocalPreference } from "./browser-storage";
import { applyTheme, defaultTheme, readStoredTheme, useTheme } from "./theme";
import { checkForUpdates, compareVersions } from "./update-check";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

it("tolerates blocked preferences and preserves normal storage behavior", () => {
  const descriptor = Object.getOwnPropertyDescriptor(window, "localStorage");
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    get: () => {
      throw new DOMException("denied", "SecurityError");
    },
  });
  expect([readLocalPreference("theme"), writeLocalPreference("theme", "dark"), removeLocalPreference("theme")]).toEqual([
    null,
    false,
    false,
  ]);
  Object.defineProperty(window, "localStorage", descriptor);
  expect(writeLocalPreference("theme", "light")).toBe(true);
  expect(readLocalPreference("theme")).toBe("light");
  expect(removeLocalPreference("theme")).toBe(true);
});

it("treats preferences as unavailable without a browser global", () => {
  vi.stubGlobal("window", undefined);
  expect([readLocalPreference("theme"), writeLocalPreference("theme", "dark"), removeLocalPreference("theme")]).toEqual([
    null,
    false,
    false,
  ]);
  vi.unstubAllGlobals();
});

it("reads, applies, and toggles the supported theme", () => {
  window.localStorage.setItem("aipermission-theme", "invalid");
  expect(readStoredTheme()).toBe(defaultTheme);
  applyTheme("dark");
  expect(document.documentElement.dataset.theme).toBe("dark");
  const { result } = renderHook(() => useTheme());
  act(() => result.current.toggleTheme());
  expect(result.current.theme).toBe("light");
  expect(window.localStorage.getItem("aipermission-theme")).toBe("light");
  act(() => result.current.setTheme("dark"));
  expect(result.current.theme).toBe("dark");
});

it("follows SemVer prerelease precedence without numeric coercion", () => {
  const ordered = [
    "1.0.0-alpha",
    "1.0.0-alpha.1",
    "1.0.0-alpha.beta",
    "1.0.0-beta",
    "1.0.0-beta.2",
    "1.0.0-beta.11",
    "1.0.0-rc.1",
    "1.0.0",
  ];
  ordered.slice(1).forEach((version, index) => expect(compareVersions(version, ordered[index])).toBe(1));
  expect(compareVersions("1.0.0-rc.10", "1.0.0-rc.2")).toBe(1);
  expect(compareVersions("1.0.0+build.2", "1.0.0+build.1")).toBe(0);
  expect(compareVersions("v2.0", "1.999.999")).toBe(1);
});

it("checks stable releases and falls back to the release list", async () => {
  const release = (version) => ({ tag_name: `v${version}`, html_url: `https://example.test/${version}` });
  const fetch = vi.fn().mockResolvedValueOnce({ ok: true, json: async () => release("0.2.54") });
  vi.stubGlobal("fetch", fetch);
  await expect(checkForUpdates("0.2.53")).resolves.toMatchObject({ latestVersion: "0.2.54", updateAvailable: true });
  fetch.mockResolvedValueOnce({ ok: false, status: 404 }).mockResolvedValueOnce({ ok: true, json: async () => [release("0.2.54-rc.1")] });
  await expect(checkForUpdates("0.2.54")).resolves.toMatchObject({ latestVersion: "0.2.54-rc.1", updateAvailable: false });
});

it("rejects release API failures and empty fallbacks", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce({ ok: false, status: 503 }));
  await expect(checkForUpdates("0.2.53")).rejects.toThrow("503");
  fetch
    .mockReset()
    .mockResolvedValueOnce({ ok: false, status: 404 })
    .mockResolvedValueOnce({ ok: true, json: async () => [] });
  await expect(checkForUpdates("0.2.53")).rejects.toThrow(/No GitHub releases/);
});
