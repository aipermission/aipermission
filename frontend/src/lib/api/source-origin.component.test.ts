import { afterEach, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.resetModules();
});

it.each(["http://127.0.0.1:3213", "http://localhost:3213", "http://[::1]:3213"])(
  "keeps no-env API calls same-site with %s",
  async (origin) => {
    vi.stubEnv("VITE_API_URL", undefined);
    vi.stubGlobal("window", { location: new URL(origin) });
    vi.resetModules();
    const { apiUrl } = await import("../api");
    const expected = new URL(origin);
    expected.port = "8080";
    expect(apiUrl).toBe(expected.origin);
  },
);

it.each(["", "http://127.0.0.1:18080/", "https://localhost:8443/"])(
  "preserves explicit API override %j including production relative mode",
  async (override) => {
    vi.stubEnv("VITE_API_URL", override);
    vi.resetModules();
    const { apiUrl } = await import("../api");
    expect(apiUrl).toBe(override.replace(/\/+$/, ""));
  },
);

it("keeps a deterministic non-browser development default", async () => {
  vi.stubEnv("VITE_API_URL", undefined);
  vi.stubGlobal("window", undefined);
  vi.resetModules();
  expect((await import("../api")).apiUrl).toBe("http://localhost:8080");
});
