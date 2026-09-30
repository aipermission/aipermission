import { readFileSync } from "node:fs";
import { expect } from "@playwright/test";

export async function observeSQLBrowserRuntime(page) {
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error" && /Content Security Policy|worker/i.test(message.text())) errors.push(message.text());
  });
  const nginx = readFileSync(new URL("../nginx.conf", import.meta.url), "utf8");
  const policy = nginx.match(/add_header Content-Security-Policy "([^"]+)"/)[1];
  // The mock API lives on a separate loopback origin; script/worker policy stays unchanged.
  await page.route("**/", async (route) => {
    if (!route.request().isNavigationRequest()) return route.continue();
    const response = await route.fetch();
    await route.fulfill({
      response,
      headers: {
        ...response.headers(),
        "content-security-policy": policy.replace("connect-src 'self'", "connect-src 'self' http://localhost:8080"),
      },
    });
  });
  return errors;
}

export async function verifySQLBrowserRuntime(page, testInfo, manualQueries, errors) {
  const editor = page.locator('[aria-label="SQL editor"] .monaco-editor');
  await expect(editor).toBeVisible();
  const input = editor.getByRole("textbox", { name: "Editor content" });
  for (const acceptKey of ["Enter", "Tab"]) {
    await editor.click({ position: { x: 180, y: 20 } });
    await expect(input).toBeFocused();
    await page.keyboard.press("ControlOrMeta+A");
    await page.keyboard.type("sel", { delay: 50 });
    await expect(editor.locator(".view-lines")).toContainText("sel");
    await expect(page.locator(".suggest-widget.visible").getByText("SELECT", { exact: true }).first()).toBeVisible();
    await page.keyboard.press(acceptKey);
    await expect(page.locator(".suggest-widget.visible")).toHaveCount(0);
    await expect(editor.locator(".view-lines")).toContainText(/select/i);
  }
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.type("SELECT 1", { delay: 50 });
  await page.keyboard.press("Escape");
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect.poll(() => manualQueries.map((request) => request.input.sql)).toEqual(["SELECT 1"]);
  await expect(input).toBeFocused();

  await verifyEditorWorkerRPC(page);

  for (const width of [1920, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(editor).toBeVisible();
    const box = await editor.boundingBox();
    expect(box.width).toBeGreaterThan(100);
    expect(box.height).toBeGreaterThan(50);
    await page.screenshot({ path: testInfo.outputPath(`sql-editor-${width}.png`), fullPage: true });
  }
  await page.setViewportSize({ width: 1920, height: 1080 });
  expect(errors).toEqual([]);
}

async function verifyEditorWorkerRPC(page) {
  const response = page.waitForResponse((item) => /editor\.worker-.*\.js/.test(item.url()));
  const result = await page.evaluate(async () => {
    const worker = window.MonacoEnvironment.getWorker("sql-browser-test", "editorWorkerService");
    let timer;
    try {
      return await new Promise((resolve, reject) => {
        timer = window.setTimeout(() => reject(new Error("SQL worker RPC timed out")), 5000);
        worker.onerror = (event) => reject(new Error(event.message));
        worker.onmessage = ({ data }) => {
          if (data.err) return reject(new Error(data.err.message));
          if (data.type !== 1) return;
          if (data.seq === "initialize") {
            worker.postMessage({ type: 0, vsWorker: 1, req: "ping", channel: "default", method: "$ping", args: [] });
          } else if (data.seq === "ping") resolve(data.res);
        };
        // Monaco's first message bootstraps RPC; initialize then ping the editor worker.
        worker.postMessage("editorWorkerService");
        worker.postMessage({ type: 0, vsWorker: 1, req: "initialize", channel: "default", method: "$initialize", args: [1] });
      });
    } finally {
      window.clearTimeout(timer);
      worker.terminate();
    }
  });
  expect(result).toBe("pong");
  expect((await response).ok()).toBe(true);
}
