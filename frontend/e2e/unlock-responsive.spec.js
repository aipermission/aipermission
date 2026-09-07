import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

for (const width of [320, 360]) {
  for (const databasePresent of [true, false]) {
    test(`@accessibility keeps ${databasePresent ? "unlock" : "setup"} tabs usable at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: width === 320 ? 568 : 800 });
      await page.route("http://localhost:8080/api/unlock/status", async (route) => {
        await route.fulfill({
          json: databasePresent
            ? {
                state: "session_required",
                database_id: "default",
                databases: [{ id: "default", name: "Default", state: "locked" }],
              }
            : { state: "setup_required", database_id: "default", databases: [] },
        });
      });
      await page.goto("/");

      const labels = databasePresent
        ? ["Unlock Database", "New Database", "Import Database", "Restore Remote"]
        : ["Create Database", "Import Database", "Restore Remote"];
      for (const label of labels) {
        const tab = page.getByRole("button", { name: label, exact: true });
        await expect(tab).toBeVisible();
        expect(await tab.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
        await tab.click();
        const results = await new AxeBuilder({ page }).analyze();
        const violations = results.violations.filter(({ impact }) => ["moderate", "serious", "critical"].includes(impact));
        expect(violations, violations.map(({ id, help }) => `${id}: ${help}`).join("\n")).toEqual([]);
      }

      await expect(page.locator("html")).toHaveJSProperty("scrollWidth", width);
    });
  }
}
