import { readFileSync } from "node:fs";
import { expect, test as base } from "@playwright/test";

const data = JSON.parse(readFileSync(new URL("./mock-api-responses.json", import.meta.url), "utf8"));

export function mockResponse(name) {
  if (!Object.hasOwn(data.responses, name)) throw new Error(`Unknown mock response: ${name}`);
  return structuredClone(data.responses[name]);
}

export const test = base.extend({
  serviceWorkers: "block",
  apiIsolation: [
    async ({ context }, use) => {
      const unexpectedCalls = [];
      const isolation = {
        unexpectedCalls,
        async block(route) {
          unexpectedCalls.push(`${route.request().method()} ${route.request().url()}`);
          await route.abort("blockedbyclient");
        },
        async route(page, url, methods, handler) {
          await page.route(url, async (route) => {
            if (!methods.includes(route.request().method())) return isolation.block(route);
            await handler(route);
          });
        },
        assertClean() {
          expect(unexpectedCalls, "Unexpected mock API calls (no live API fallback is allowed)").toEqual([]);
        },
      };
      const isAPI = (url) => {
        let path = url.pathname;
        try {
          path = decodeURIComponent(path);
        } catch {
          // Invalid escapes must not prevent matching a literal API prefix.
        }
        return ["http://localhost:8080", "ws://localhost:8080"].includes(url.origin) || path === "/api" || path.startsWith("/api/");
      };
      // Context routes cover every page; later page fixtures override this deny boundary.
      await context.route(isAPI, isolation.block);
      await context.routeWebSocket(isAPI, (socket) => {
        unexpectedCalls.push(`WEBSOCKET ${socket.url()}`);
        socket.close({ code: 1008, reason: "Unexpected mock API websocket" });
      });
      await use(isolation);
    },
    { auto: true },
  ],
});

test.afterEach(async ({ apiIsolation }) => {
  apiIsolation.assertClean();
});

export async function installStaticMockRoutes(page, isolation) {
  for (const [path, response] of Object.entries(structuredClone(data.staticRoutes))) {
    await isolation.route(page, `http://localhost:8080${path}`, ["GET"], async (route) => {
      await route.fulfill(response);
    });
  }
}
