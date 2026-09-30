import { expect } from "@playwright/test";
import { cleanupFixture } from "../src/connectors/templates/ssh/cleanup-test-fixtures";

export async function verifySSHCleanupBrowser({ page, testInfo, width, unlock, expectNoModerateAccessibilityViolations }) {
  const { wire, acknowledgement } = cleanupFixture(1);
  let mutations = 0;
  await page.route("http://localhost:8080/api/connector-targets/1/operations/key-cleanup-status", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({});
    await route.fulfill({ json: wire });
  });
  await page.route("http://localhost:8080/api/connector-targets/1/operations/key-cleanup-attest", async (route) => {
    mutations += 1;
    const input = route.request().postDataJSON();
    expect(input).toMatchObject({
      resource_id: 41,
      generation: wire.records[0].entry.record.generation,
      deletion_context_digest: wire.deletion_context_digest,
      identity_digest: wire.records[0].choices[0].digest,
      reason: "Externally verified",
    });
    expect(input.coverage).toEqual(
      wire.records[0].choices[0].subjects.map((subject) => ({
        subject_id: subject.id,
        method: "provider_console",
        absent: true,
        reason: "Provider verified absence",
      })),
    );
    Object.assign(acknowledgement.entry.record.attestations.at(-1), { reason: input.reason, coverage: input.coverage });
    wire.records[0].entry = acknowledgement.entry;
    await route.fulfill({ status: 409, json: { error: "The proof may have committed; reload current evidence." } });
  });
  await unlock(page);
  await page.setViewportSize({ width, height: 900 });
  await page.goto("/connectors");
  await page.getByTitle("Reconcile key cleanup").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: "Record external absence" })).toBeVisible();
  await expect(dialog.getByRole("checkbox")).toHaveCount(2);
  expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  const bounds = await dialog.boundingBox();
  expect(bounds.x).toBeGreaterThanOrEqual(0);
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(900);
  for (let index = 0; index < 2; index += 1) {
    await expect(dialog.getByRole("checkbox").nth(index)).not.toBeChecked();
    await dialog.getByLabel("Location evidence").nth(index).fill("Provider verified absence");
    await dialog.getByRole("checkbox").nth(index).check();
  }
  await dialog.getByLabel("Decision reason").fill("Externally verified");
  await dialog.getByRole("button", { name: "Record external absence" }).click();
  await expect(dialog.getByText(/outcome was not confirmed/)).toBeVisible();
  await expect(dialog.getByLabel("Cleanup record")).toHaveValue("41");
  await expect(dialog.getByLabel("Cleanup record")).toContainText("attested");
  await expect(dialog.getByLabel("Decision reason")).toHaveValue("");
  await expect(dialog.getByRole("checkbox").nth(0)).not.toBeChecked();
  const history = dialog.getByRole("region", { name: "Recorded cleanup evidence" });
  await history.locator("summary").filter({ hasText: "Decision 2:" }).click();
  await expect(history.getByText("Externally verified", { exact: true })).toBeVisible();
  await expect(history.getByTitle("Copy recorded decision 2")).toBeVisible();
  const proof = history.getByRole("textbox", { name: "Recorded decision 2 JSON" });
  expect(JSON.parse(await proof.inputValue())).toEqual(acknowledgement.entry.record.attestations[1]);
  await proof.focus();
  await expect(proof).toBeFocused();
  expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  expect(mutations).toBe(1);
  await expectNoModerateAccessibilityViolations(page, '[role="dialog"]');
  await page.screenshot({ path: testInfo.outputPath(`ssh-cleanup-${width}.png`) });
}
