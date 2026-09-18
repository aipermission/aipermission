import { defineConfig, devices } from "@playwright/test";

const port = process.env.AIPERMISSION_PLAYWRIGHT_PORT || "4173";
if (!/^\d+$/.test(port) || Number(port) < 1024 || Number(port) > 65535) {
  throw new Error("AIPERMISSION_PLAYWRIGHT_PORT must be a valid non-privileged port");
}
const origin = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  reporter: [["line"], ["./scripts/no-skipped-playwright-reporter.mjs"]],
  timeout: 30_000,
  forbidOnly: true,
  retries: 0,
  expect: {
    timeout: 5_000,
  },
  use: {
    baseURL: origin,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: `npm run build && npm run preview -- --host 127.0.0.1 --strictPort --port ${port}`,
    url: origin,
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
