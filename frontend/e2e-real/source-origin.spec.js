import { expect, test } from "@playwright/test";

test("creates, locks and unlocks a real encrypted database through source Vite defaults", async ({ page }) => {
  const mutations = [];
  page.on("response", (response) => {
    if (response.request().method() === "POST") mutations.push({ url: response.url(), status: response.status() });
  });
  await page.goto("/");
  await page.locator('input[type="password"]').nth(0).fill("SourceOriginPassword123");
  await page.locator('input[type="password"]').nth(1).fill("SourceOriginPassword123");
  await page.getByRole("button", { name: "Create encrypted database" }).click();
  await expect(page.locator('aside a[href="/console"]')).toBeVisible();
  await page.getByRole("button", { name: "Lock", exact: true }).click();
  await expect(page.getByRole("button", { name: "Unlock", exact: true })).toBeVisible();
  await page.locator('input[type="password"]').fill("SourceOriginPassword123");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(page.locator('aside a[href="/console"]')).toBeVisible();
  expect(mutations.length).toBeGreaterThanOrEqual(3);
  expect(mutations.every(({ url, status }) => url.startsWith("http://127.0.0.1:8080/") && status >= 200 && status < 300)).toBe(true);
});
