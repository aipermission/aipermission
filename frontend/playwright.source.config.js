import { defineConfig } from "@playwright/test";
import realBackend from "./playwright.real.config.js";

export default defineConfig({
  ...realBackend,
  testIgnore: [],
  testMatch: "source-origin.spec.js",
  use: { ...realBackend.use, baseURL: "http://127.0.0.1:3213" },
  webServer: [
    {
      ...realBackend.webServer[0],
      command: "AIPERMISSION_E2E_BACKEND_PORT=8080 AIPERMISSION_E2E_FRONTEND_PORT=3213 ../scripts/run-real-e2e-backend.sh",
      url: "http://127.0.0.1:8080/__e2e/ready",
    },
    {
      command: "env -u VITE_API_URL npm run dev -- --strictPort --port 3213",
      url: "http://127.0.0.1:3213",
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
});
