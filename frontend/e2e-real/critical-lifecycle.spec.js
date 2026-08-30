import { expect, test } from "@playwright/test";

const databasePassword = "RealBackendPassword123";

test("runs approval, stale rejection, lock, and restart against the real backend", async ({ page }) => {
  await test.step("create and unlock an encrypted database", async () => {
    await page.goto("/");
    await page.locator('input[type="password"]').nth(0).fill(databasePassword);
    await page.locator('input[type="password"]').nth(1).fill(databasePassword);
    await page.getByRole("button", { name: "Create encrypted database" }).click();
    await expect(page.locator('aside a[href="/console"]')).toBeVisible();
    await page.getByRole("button", { name: "Start MCP" }).click();
    await expect(page.getByRole("button", { name: "Stop MCP" })).toBeVisible();
  });

  const fixture = await seedConnectorFixture(page);
  await page.goto(`/console?target=${encodeURIComponent(fixture.targetRef)}`);
  await expect(page.getByText("e2e-target", { exact: true }).first()).toBeVisible();

  await test.step("approve a Prompt connector action and persist its completion", async () => {
    const request = await callConnectorAction(page, fixture.token, fixture.targetRef, "approved message");
    expect(request.status).toBe("approval_pending");

    await expect(page.getByRole("dialog", { name: "e2e action approval" })).toBeVisible();
    await page.getByRole("button", { name: "Run", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "e2e action approval" })).not.toBeVisible();

    const completed = await mcpRequest(page, fixture.token, `/api/mcp/connector-action-requests/${request.request_id}`);
    expect(completed.status).toBe("completed");
    expect(completed.output).toEqual({ message: "approved message" });
  });

  await test.step("reject a pending action when its permission context drifts", async () => {
    const request = await callConnectorAction(page, fixture.token, fixture.targetRef, "stale message");
    expect(request.status).toBe("approval_pending");
    await expect(page.getByRole("dialog", { name: "e2e action approval" })).toBeVisible();

    await uiRequest(page, `/api/tokens/${fixture.token.id}/connector-permissions`, "PUT", {
      permissions: [{ target_id: fixture.targetID, profile_id: fixture.profileID, action_name: "echo", execution_rule: "blocked" }],
    });
    await page.getByRole("button", { name: "Run", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "e2e action approval" }).getByText(/approval context changed/i)).toBeVisible();
    await page.getByRole("button", { name: "OK", exact: true }).click();

    const stale = await mcpRequest(page, fixture.token, `/api/mcp/connector-action-requests/${request.request_id}`);
    expect(stale.status).toBe("stale");
  });

  await test.step("lock, unlock, and survive a backend restart with the same encrypted database", async () => {
    await page.getByRole("button", { name: "Lock", exact: true }).click();
    await expect(page.getByRole("button", { name: "Unlock", exact: true })).toBeVisible();
    await page.locator('input[type="password"]').fill(databasePassword);
    await page.getByRole("button", { name: "Unlock", exact: true }).click();
    await expect(page.locator('aside a[href="/console"]')).toBeVisible();

    const restart = await page.request.post("http://127.0.0.1:18080/__e2e/restart");
    expect(restart.ok()).toBe(true);
    await page.reload();
    await expect(page.getByRole("button", { name: "Unlock", exact: true })).toBeVisible();
    await page.locator('input[type="password"]').fill(databasePassword);
    await page.getByRole("button", { name: "Unlock", exact: true }).click();
    await expect(page.getByText("e2e-target", { exact: true }).first()).toBeVisible();
  });
});

async function seedConnectorFixture(page) {
  const target = await uiRequest(page, "/api/connector-targets/with-profile", "POST", {
    target: { connector_kind: "e2e", name: "e2e-target", config: {} },
    profile: { kind: "local", label: "local", public: {}, secret: {}, risk_label: "read-only" },
  });
  const token = await uiRequest(page, "/api/tokens", "POST", { name: "real-browser-agent" });
  const profile = target.profiles[0];
  await uiRequest(page, `/api/tokens/${token.id}/connector-permissions`, "PUT", {
    permissions: [{ target_id: target.id, profile_id: profile.id, action_name: "echo", execution_rule: "approval_required" }],
  });
  return { token, targetID: target.id, profileID: profile.id, targetRef: `e2e:${target.id}:${profile.id}` };
}

async function callConnectorAction(page, token, targetRef, message) {
  return mcpRequest(page, token, "/api/mcp/connector-actions/call", "POST", {
    target_ref: targetRef,
    action_name: "echo",
    input: { message },
    reason: "real browser lifecycle regression",
  });
}

async function mcpRequest(page, token, path, method = "GET", body) {
  return page.evaluate(
    async ({ tokenValue, requestPath, requestMethod, requestBody }) => {
      const response = await fetch(`http://127.0.0.1:18080${requestPath}`, {
        method: requestMethod,
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${tokenValue}` },
        body: requestBody === undefined ? undefined : JSON.stringify(requestBody),
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || `MCP request failed: ${response.status}`);
      return data;
    },
    { tokenValue: token.token, requestPath: path, requestMethod: method, requestBody: body },
  );
}

async function uiRequest(page, path, method, body) {
  return page.evaluate(
    async ({ requestPath, requestMethod, requestBody }) => {
      const csrfCookie = document.cookie.split("; ").find((entry) => entry.startsWith("aipermission_csrf_4174="));
      const csrf = csrfCookie ? decodeURIComponent(csrfCookie.split("=").slice(1).join("=")) : "";
      const response = await fetch(`http://127.0.0.1:18080${requestPath}`, {
        method: requestMethod,
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-AIPermission-CSRF": csrf },
        body: JSON.stringify(requestBody),
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || `UI request failed: ${response.status}`);
      return data;
    },
    { requestPath: path, requestMethod: method, requestBody: body },
  );
}
