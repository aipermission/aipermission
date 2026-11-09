import { expect } from "@playwright/test";
import { test, installStaticMockRoutes, fulfillWorkspace, mockResponse, workspaceHeaders } from "../mock-browser.mjs";

test("console capacity updates and guarded cleanup stay usable on desktop/mobile @high-risk", async ({ page, apiIsolation }, testInfo) => {
  await installStaticMockRoutes(page, apiIsolation);
  await apiIsolation.route(page, "http://localhost:8080/api/unlock/status", ["GET"], (route) =>
    fulfillWorkspace(route, { headers: workspaceHeaders, json: mockResponse("unlockedStatus") }),
  );
  let items = mockResponse("consoleCapacityItems");
  let revision = 1;
  let policy = { enabled: false, idle_minutes: 60 };
  await apiIsolation.route(page, "http://localhost:8080/api/settings/console-cleanup", ["GET", "PUT"], async (route) => {
    if (route.request().method() === "PUT") {
      policy = route.request().postDataJSON();
      expect(policy).toEqual({ enabled: true, idle_minutes: 30 });
    }
    await fulfillWorkspace(route, { json: policy });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/console/capacity", ["GET"], (route) =>
    fulfillWorkspace(route, {
      json: { limit: 32, used: items.length, available: 32 - items.length, revision, observed_at: "2026-11-08T12:00:00Z", items },
    }),
  );
  await apiIsolation.route(page, "http://localhost:8080/api/console/capacity/close", ["POST"], async (route) => {
    const body = route.request().postDataJSON();
    expect(body).toEqual({ idle_minutes: 30, sessions: [{ id: 1, generation: 1, last_activity: "2026-11-08T10:00:00Z" }] });
    items = items.filter((item) => item.id !== 1);
    revision++;
    await fulfillWorkspace(route, { json: { closed: [1], skipped: [], uncertain: [] } });
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/");
  await expect(page.getByRole("button", { name: /Capacity 2 \/ 32/ })).toBeVisible();
  await expect(page.getByRole("progressbar", { name: "Console capacity" })).toHaveJSProperty("value", 6.25);
  await page.getByRole("button", { name: /Capacity 2 \/ 32/ }).click();
  const dialog = page.getByRole("dialog", { name: "Console capacity", exact: true });
  await expect(dialog.getByText("My Container", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Close session 2" })).toBeDisabled();
  await expect(dialog.getByLabel("Idle threshold")).toHaveValue("30");
  await page.screenshot({ path: testInfo.outputPath("capacity-desktop.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(dialog).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("capacity-mobile.png"), fullPage: true });
  await dialog.getByRole("button", { name: "Clean 1 sessions" }).click();
  await page
    .getByRole("dialog", { name: "Close 1 console sessions?" })
    .getByRole("button", { name: "Close sessions", exact: true })
    .click();
  await expect(dialog.getByText(/1 closed/)).toBeVisible();
  await expect(dialog.getByText("My Container", { exact: true })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page.goto("/settings");
  await expect(page.getByLabel("Automatic idle cleanup")).not.toBeChecked();
  await page.getByLabel("Automatic idle cleanup").check();
  await page.getByLabel("Idle threshold").selectOption("30");
  await page.getByRole("button", { name: "Save console cleanup" }).click();
  await expect(page.getByText(/Next check is at the start of the next hour/)).toBeVisible();
});
