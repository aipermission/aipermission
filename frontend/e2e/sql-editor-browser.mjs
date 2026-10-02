import { readFileSync, writeFileSync } from "node:fs";
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

export async function verifySQLBrowserRuntime(page, testInfo, manualQueries, errors, connectorKind) {
  const editor = page.locator('[aria-label="SQL editor"] .monaco-editor');
  await expect(editor).toBeVisible();
  await verifyEditorWorkerRPC(page);
  const expectedQueries = [];
  for (const width of [1920, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await verifySQLKeyboard(page, editor, manualQueries, expectedQueries);
    const path = testInfo.outputPath(`${connectorKind}-sql-editor-${width}.png`);
    await page.screenshot({ path, fullPage: true });
    await testInfo.attach(`${connectorKind}-sql-editor-${width}`, { path, contentType: "image/png" });
    const bounds = await verifySQLControlBounds(page, editor, width);
    const boundsPath = testInfo.outputPath(`${connectorKind}-sql-control-bounds-${width}.json`);
    writeFileSync(boundsPath, JSON.stringify(bounds, null, 2));
    await testInfo.attach(`${connectorKind}-sql-control-bounds-${width}`, { path: boundsPath, contentType: "application/json" });
  }
  await page.setViewportSize({ width: 1920, height: 1080 });
  expect(errors).toEqual([]);
}

async function verifySQLKeyboard(page, editor, manualQueries, expectedQueries) {
  const input = editor.getByRole("textbox", { name: "Editor content" });
  const lines = editor.locator(".view-lines");
  const form = page.locator("form").filter({ has: editor });
  const runButton = form.getByRole("button", { name: "Run SQL (Ctrl+Enter)", exact: true });
  await form.getByRole("button", { name: "Last query", exact: true }).click();
  await expect(lines).toHaveText("SELECT 2", { useInnerText: true });
  await expect(input).toBeFocused();
  for (const acceptKey of ["Enter", "Tab", "Enter", "Tab"]) {
    await clearSQLEditor(input, lines, runButton);
    await page.keyboard.type("sel", { delay: 50 });
    await expect(lines).toHaveText("sel", { useInnerText: true });
    await expect(page.locator(".suggest-widget.visible .monaco-list-row.focused").getByText("SELECT", { exact: true })).toBeVisible();
    await input.press(acceptKey);
    await expect(page.locator(".suggest-widget.visible")).toHaveCount(0);
    await expect.poll(() => lines.innerText()).toBe("select");
    await expect(input).toBeFocused();
  }
  await clearSQLEditor(input, lines, runButton);
  await page.keyboard.type("SELECT 1", { delay: 50 });
  await input.press("Escape");
  await expect(page.locator(".suggest-widget.visible")).toHaveCount(0);
  await expect(lines).toHaveText("SELECT 1", { useInnerText: true });
  await input.press("ControlOrMeta+Enter");
  expectedQueries.push("SELECT 1");
  await expect.poll(() => manualQueries.map((request) => request.input.sql)).toEqual(expectedQueries);
  await expect(runButton).toBeEnabled();
  await expect(input).toBeFocused();
  await expect(lines).toHaveText("SELECT 1", { useInnerText: true });
}

async function clearSQLEditor(input, lines, runButton) {
  // Acceptance changes Monaco's selection; settle the empty controlled draft before typing again.
  await input.focus();
  await expect(input).toBeFocused();
  await input.press("Escape");
  await input.press("ControlOrMeta+A");
  await input.press("Backspace");
  await expect(lines).toHaveText("", { useInnerText: true });
  await expect(runButton).toBeDisabled();
}

async function verifySQLControlBounds(page, editor, width) {
  const form = page.locator("form").filter({ has: editor });
  const header = page.locator("header").filter({ has: page.getByRole("button", { name: "New Session", exact: true }) });
  const bounds = { width, containers: {}, controls: {} };
  for (const [name, container, controls] of [
    [
      "query",
      form,
      [
        ["editor", editor],
        ["metadata", form.locator("p").last()],
        ["copy", form.getByTitle("Copy SQL", { exact: true })],
        ["lastQuery", form.getByRole("button", { name: "Last query", exact: true })],
        ["maxRows", form.getByLabel("Max rows", { exact: true }).locator("..")],
        ["run", form.getByRole("button", { name: "Run SQL (Ctrl+Enter)", exact: true })],
      ],
    ],
    [
      "session",
      header,
      [
        ["newSession", header.getByRole("button", { name: "New Session", exact: true })],
        ["endSession", header.getByRole("button", { name: "End Session", exact: true })],
        ["profile", header.getByRole("combobox", { name: "Profile", exact: true })],
      ],
    ],
  ]) {
    const containerBox = await container.boundingBox();
    expect(containerBox).not.toBeNull();
    expect(containerBox.x, `${name} left`).toBeGreaterThanOrEqual(0);
    expect(containerBox.x + containerBox.width, `${name} right`).toBeLessThanOrEqual(width);
    bounds.containers[name] = containerBox;
    for (const [controlName, control] of controls) {
      await expect(control).toBeVisible();
      const box = await control.boundingBox();
      expect(box).not.toBeNull();
      expect(box.width, `${controlName} width`).toBeGreaterThan(0);
      expect(box.height, `${controlName} height`).toBeGreaterThan(0);
      expect(box.x, `${controlName} left`).toBeGreaterThanOrEqual(containerBox.x);
      expect(box.x + box.width, `${controlName} right`).toBeLessThanOrEqual(containerBox.x + containerBox.width);
      expect(box.y, `${controlName} top`).toBeGreaterThanOrEqual(containerBox.y);
      expect(box.y + box.height, `${controlName} bottom`).toBeLessThanOrEqual(containerBox.y + containerBox.height);
      bounds.controls[controlName] = box;
    }
  }
  expect(bounds.controls.editor.width).toBeGreaterThan(100);
  expect(bounds.controls.editor.height).toBeGreaterThan(50);
  const controls = Object.entries(bounds.controls);
  for (let index = 0; index < controls.length; index++) {
    const [leftName, left] = controls[index];
    for (const [rightName, right] of controls.slice(index + 1)) {
      const overlap =
        left.x < right.x + right.width &&
        right.x < left.x + left.width &&
        left.y < right.y + right.height &&
        right.y < left.y + left.height;
      expect(overlap, `${leftName} overlaps ${rightName}`).toBe(false);
    }
  }
  return bounds;
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
