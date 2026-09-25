import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { checkForUpdates, compareVersions } from "./update-check";

const fetchMock = vi.fn<typeof fetch>();
const releaseURL = "https://github.com/aipermission/aipermission/releases/tag/v0.2.62";
function jsonResponse(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => vi.unstubAllGlobals());

it("checks a valid tagged release and preserves its public URL", async () => {
  fetchMock.mockResolvedValue(jsonResponse({ tag_name: "v0.2.62", html_url: releaseURL }));
  await expect(checkForUpdates("0.2.61")).resolves.toEqual({
    latestVersion: "0.2.62",
    localVersion: "0.2.61",
    releaseUrl: releaseURL,
    updateAvailable: true,
  });
  expect(fetchMock).toHaveBeenCalledExactlyOnceWith("https://api.github.com/repos/aipermission/aipermission/releases/latest", {
    headers: { Accept: "application/vnd.github+json" },
  });
});

it("falls back to the bounded release list only when latest returns 404", async () => {
  fetchMock.mockResolvedValueOnce(jsonResponse({}, 404)).mockResolvedValueOnce(jsonResponse([{ name: "v0.2.60" }]));
  await expect(checkForUpdates(" v0.2.61 ")).resolves.toEqual({
    latestVersion: "0.2.60",
    localVersion: "0.2.61",
    releaseUrl: "https://github.com/aipermission/aipermission/releases",
    updateAvailable: false,
  });
  expect(fetchMock).toHaveBeenNthCalledWith(
    2,
    "https://api.github.com/repos/aipermission/aipermission/releases?per_page=1",
    expect.anything(),
  );
});

it.each([null, [], { tag_name: 1 }, { html_url: {} }])("rejects unchecked release metadata %j", async (data) => {
  fetchMock.mockResolvedValue(jsonResponse(data));
  await expect(checkForUpdates("0.2.61")).rejects.toThrow("Invalid GitHub release response.");
  expect(fetchMock).toHaveBeenCalledOnce();
});

it.each([{}, [], [null], [{ name: false }]])("rejects an unusable fallback release collection %j", async (data) => {
  fetchMock.mockResolvedValueOnce(jsonResponse({}, 404)).mockResolvedValueOnce(jsonResponse(data));
  await expect(checkForUpdates("0.2.61")).rejects.toThrow("No GitHub releases found.");
  expect(fetchMock).toHaveBeenCalledTimes(2);
});

it.each(["latest", "list"])("does not turn a failed %s response into a successful version check", async (route) => {
  if (route === "list") fetchMock.mockResolvedValueOnce(jsonResponse({}, 404));
  fetchMock.mockResolvedValueOnce(jsonResponse({}, 503));
  await expect(checkForUpdates("0.2.61")).rejects.toThrow("GitHub release check failed with 503");
  expect(fetchMock).toHaveBeenCalledTimes(route === "list" ? 2 : 1);
});

it.each([
  { left: "1.0.0", right: "0.99.99", expected: 1 },
  { left: "1.2.0", right: "1.3.0", expected: -1 },
  { left: "1.2.3", right: "1.2.3+build", expected: 0 },
  { left: "1.2.3", right: "1.2.3-rc.1", expected: 1 },
  { left: "1.2.3-rc.1", right: "1.2.3", expected: -1 },
  { left: "1.0.0-alpha", right: "1.0.0-alpha.1", expected: -1 },
  { left: "1.0.0-alpha.1", right: "1.0.0-alpha", expected: 1 },
  { left: "1.0.0-1", right: "1.0.0-alpha", expected: -1 },
  { left: "1.0.0-alpha", right: "1.0.0-1", expected: 1 },
  { left: "1.0.0-beta", right: "1.0.0-alpha", expected: 1 },
  { left: "1.0.0-alpha", right: "1.0.0-beta", expected: -1 },
  { left: "1.0.0-rc.1", right: "1.0.0-rc.1", expected: 0 },
  { left: "1.0.0-rc.10", right: "1.0.0-rc.2", expected: 1 },
  { left: "9007199254740993.0.0", right: "9007199254740992.0.0", expected: 1 },
  { left: "1.2", right: "1.2.0", expected: 0 },
])("compares $left and $right without numeric precision loss", ({ left, right, expected }) => {
  expect(compareVersions(left, right)).toBe(expected);
});
