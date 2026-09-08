import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { responsiveViewportMatrix } from "../scripts/playwright-gate-manifest.mjs";

test.beforeEach(async ({ page }) => {
  let unlocked = false;
  let connectorPermissions = [];
  let connectorPermissionRevision = "connector-permissions-1";
  let projectCapabilities = [];
  let projectCapabilityRevision = "project-capabilities-1";
  let enabledProjectIDs = [1];
  let projectScopeRevision = "project-scopes-1";
  let mcpRuntimeEnabled = false;
  await page.route("http://localhost:8080/api/unlock/status", async (route) => {
    await route.fulfill({
      json: unlocked
        ? unlockedStatus()
        : {
            state: "session_required",
            database_id: "default",
            databases: [{ id: "default", name: "Default", state: "locked" }],
          },
    });
  });
  await page.route("http://localhost:8080/api/unlock", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({ database_id: "default", password: "local-password" });
    unlocked = true;
    await route.fulfill({
      headers: {
        "set-cookie": "aipermission_ui_session=test; Path=/; SameSite=Strict",
      },
      json: { state: "unlocked", database_id: "default", database_name: "Default" },
    });
  });
  await page.route("http://localhost:8080/api/backup/import", async (route) => {
    expect(route.request().method()).toBe("POST");
    const form = await requestFormData(route.request());
    expect(form.get("database_name")).toBe("Imported project");
    expect(form.get("database_password")).toBe("ImportedPassword123");
    const databaseFile = form.get("sqlite");
    expect(databaseFile).toBeInstanceOf(File);
    expect(databaseFile.name).toBe("imported.aipdb");
    expect(await databaseFile.text()).toBe("encrypted-test-fixture");
    unlocked = true;
    await route.fulfill({ json: { state: "unlocked", database_id: "imported", database_name: "Imported project" } });
  });
  await page.route("http://localhost:8080/api/status", async (route) => {
    await route.fulfill({ json: { service: "aipermission", status: "running", config: {}, features: [] } });
  });
  await page.route("http://localhost:8080/api/targets", async (route) => {
    await route.fulfill({ json: { items: [targetProfile()] } });
  });
  await page.route("http://localhost:8080/api/connectors", async (route) => {
    await route.fulfill({ json: { items: [{ kind: "ssh", label: "SSH", version: "0.1" }] } });
  });
  await page.route("http://localhost:8080/api/connector-targets", async (route) => {
    await route.fulfill({ json: { items: [targetSummary()] } });
  });
  await page.route("http://localhost:8080/api/connector-targets/inventory", async (route) => {
    await route.fulfill({ json: { items: [targetInventory()] } });
  });
  await page.route("http://localhost:8080/api/projects", async (route) => {
    await route.fulfill({ json: { items: [project(), secondaryProject()] } });
  });
  await page.route("http://localhost:8080/api/connector-targets/1", async (route) => {
    await route.fulfill({ json: targetDetail() });
  });
  await page.route("http://localhost:8080/api/connector-targets/1/profiles/1/actions", async (route) => {
    await route.fulfill({ json: { items: [sshExecAction()] } });
  });
  await page.route("http://localhost:8080/api/connectors/ssh/credentials", async (route) => {
    await route.fulfill({ json: [{ id: 1, name: "main", key_type: "ed25519", fingerprint: "SHA256:test" }] });
  });
  await page.route("http://localhost:8080/api/tokens", async (route) => {
    await route.fulfill({ json: [{ id: 1, name: "agent", token_prefix: "aip_test", created_at: "2026-05-31T00:00:00Z" }] });
  });
  await page.route("http://localhost:8080/api/console/sessions", async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route("http://localhost:8080/api/connector-action-approvals", async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route("http://localhost:8080/api/messages", async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route("http://localhost:8080/api/settings/security", async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({
        json: { reusable_tokens: false, expose_mcp_server_metadata: true, mcp_start_enabled: false, redaction_mode: "basic" },
      });
      return;
    }
    await route.fulfill({
      json: { reusable_tokens: false, expose_mcp_server_metadata: false, mcp_start_enabled: false, redaction_mode: "basic" },
    });
  });
  await page.route("http://localhost:8080/api/settings/mcp-runtime", async (route) => {
    if (route.request().method() === "PUT") {
      mcpRuntimeEnabled = Boolean(route.request().postDataJSON().enabled);
    }
    await route.fulfill({ json: { enabled: mcpRuntimeEnabled, start_enabled: false, updated_at: "2026-05-31T00:00:00Z" } });
  });
  await page.route("http://localhost:8080/api/settings/redaction-rules", async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route("http://localhost:8080/api/settings/retention", async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({ json: { history_days: 14, audit_days: 14, console_days: 7, message_days: 7 } });
      return;
    }
    await route.fulfill({ json: { history_days: 0, audit_days: 0, console_days: 0, message_days: 0 } });
  });
  await page.route("http://localhost:8080/api/backup/providers/catalog", async (route) => {
    await route.fulfill({ json: { items: [{ provider_type: "aipermission_backup", label: "AIPermission Backup" }] } });
  });
  await page.route("http://localhost:8080/api/backup/providers", async (route) => {
    await route.fulfill({ json: { items: [] } });
  });
  await page.route("http://localhost:8080/api/history-labels", async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route("http://localhost:8080/api/history/targets", async (route) => {
    await route.fulfill({ json: { items: [] } });
  });
  await page.route(/http:\/\/localhost:8080\/api\/history\?.*/, async (route) => {
    await route.fulfill({ json: { items: [], total: 0, limit: 50, has_more: false, next_cursor: null } });
  });
  await page.route("http://localhost:8080/api/tokens/1/connector-permissions", async (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      expect(body).toEqual({
        permissions: [{ target_id: 1, profile_id: 1, action_name: "exec", execution_rule: "approval_required" }],
        expected_revision: connectorPermissionRevision,
      });
      connectorPermissions = body.permissions || [];
      connectorPermissionRevision = "connector-permissions-2";
      await route.fulfill({ json: { items: connectorPermissions, revision: connectorPermissionRevision } });
      return;
    }
    expect(route.request().method()).toBe("GET");
    await route.fulfill({ json: { items: connectorPermissions, revision: connectorPermissionRevision } });
  });
  await page.route("http://localhost:8080/api/tokens/1/project-scopes", async (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      expect(Object.keys(body).sort()).toEqual(["enabled_project_ids", "expected_revision"]);
      expect(Array.isArray(body.enabled_project_ids)).toBe(true);
      expect(body.enabled_project_ids.every(Number.isInteger)).toBe(true);
      expect(body.enabled_project_ids.every((id) => id === 1 || id === 2)).toBe(true);
      expect(body.expected_revision).toBe(projectScopeRevision);
      enabledProjectIDs = body.enabled_project_ids;
      projectScopeRevision = "project-scopes-2";
    } else {
      expect(route.request().method()).toBe("GET");
    }
    await route.fulfill({ json: { items: [projectScope(enabledProjectIDs.includes(1))], revision: projectScopeRevision } });
  });
  await page.route("http://localhost:8080/api/tokens/1/project-capabilities", async (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      expect(body).toEqual({
        capabilities: [
          { project_id: 1, capability_name: "vault.metadata.read", execution_rule: "always_run" },
          { project_id: 1, capability_name: "vault.item.generate", execution_rule: "always_run" },
        ],
        expected_revision: projectCapabilityRevision,
      });
      projectCapabilities = body.capabilities.map((capability) => ({
        ...capability,
        token_id: 1,
        project_name: "Ungrouped",
        project_slug: "ungrouped",
        project_enabled: enabledProjectIDs.includes(capability.project_id),
        revision: 1,
      }));
      projectCapabilityRevision = "project-capabilities-2";
    } else {
      expect(route.request().method()).toBe("GET");
    }
    await route.fulfill({
      json: { definitions: projectCapabilityDefinitions(), items: projectCapabilities, revision: projectCapabilityRevision },
    });
  });
});

test("@high-risk unlocks the local UI session and renders the dashboard", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Your browser session is missing or expired.")).toBeVisible();
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();

  await expect(page.locator('aside a[href="/console"]')).toBeVisible();
  await expect(page.getByRole("complementary").getByText("Gateway", { exact: true })).toBeVisible();
  await expect(page.getByRole("complementary").getByText("running", { exact: true })).toBeVisible();
  await expect(page.getByRole("complementary").getByText("Stopped", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Start MCP" }).click();
  await expect(page.getByRole("button", { name: "Stop MCP" })).toBeVisible();
});

test("renders security settings and updates MCP metadata exposure", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("link", { name: /Security/ }).click();

  await expect(page.getByRole("heading", { name: "Security" })).toBeVisible();
  await expect(page.getByText("MCP connector targets hide endpoint inventory details by default.")).toBeVisible();
  await page.getByLabel("Expose endpoint metadata to MCP").click();
  await expect(page.getByText("MCP connector targets now include endpoint metadata.")).toBeVisible();
});

test("@high-risk imports a database from the unlock screen", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Import Database" }).click();
  await page.getByPlaceholder("Restored project").fill("Imported project");
  await page.locator('input[type="file"]').setInputFiles({
    name: "imported.aipdb",
    mimeType: "application/octet-stream",
    buffer: Buffer.from("encrypted-test-fixture"),
  });
  await page.locator('input[type="password"]').fill("ImportedPassword123");
  await page.locator('form button[type="submit"]').click();

  await expect(page.locator('aside a[href="/console"]')).toBeVisible();
});

test("renders settings retention controls", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("link", { name: /Settings/ }).click();

  await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Backup", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Maintenance console" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Change password" })).toBeVisible();
  await page.getByRole("button", { name: "Add provider" }).click();
  await expect(page.getByRole("dialog", { name: "Add backup provider" })).toBeVisible();
  await page.getByRole("button", { name: "Cancel" }).click();
  await page.getByLabel("Command history days").fill("14");
  await page.getByLabel("Audit log days").fill("14");
  await page.getByRole("button", { name: "Save retention" }).click();
  await expect(page.getByText("Retention settings saved and cleanup ran.")).toBeVisible();
});

test("@accessibility keeps modal focus contained and returns it to the opener", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("link", { name: /Settings/ }).click();

  const opener = page.getByRole("button", { name: "Add provider" });
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "Add backup provider" });
  await expect(dialog).toBeVisible();
  await expectNoModerateAccessibilityViolations(page, "[role=dialog]");
  await expect(dialog.getByRole("button", { name: "Close dialog" })).toBeFocused();
  for (let index = 0; index < 12; index += 1) {
    await page.keyboard.press(index % 2 === 0 ? "Tab" : "Shift+Tab");
    await expect.poll(() => dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  }

  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
});

test("@accessibility keeps primary unlocked pages accessible", async ({ page }) => {
  await unlock(page);
  for (const path of ["/console", "/tokens", "/history", "/settings"]) {
    await page.locator(`aside a[href="${path}"]`).click();
    await expect(page).toHaveURL(new RegExp(`${path.replace("/", "\\/")}$`));
    await expectNoModerateAccessibilityViolations(page, "main");
  }
});

test("@high-risk updates token connector permission from the Tokens page", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.locator('aside a[href="/tokens"]').click();

  await page.getByRole("button", { name: "Connectors" }).click();
  const dialog = page.getByRole("dialog", { name: "agent connector permissions" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: /worker-1/ }).click();
  await dialog.getByRole("button", { name: "Prompt", exact: true }).last().click();
  await dialog.getByRole("button", { name: "Save connector permissions" }).click();
  await expect(page.getByText("Connector permissions saved.")).toBeVisible();
});

test("@high-risk updates project Vault permissions from the Tokens page", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.locator('aside a[href="/tokens"]').click();

  await page.getByRole("button", { name: "Vault", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "agent Vault permissions" });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("Ungrouped project visibility").uncheck();
  await expect(dialog.getByLabel("Ungrouped project visibility")).not.toBeChecked();
  await dialog.getByLabel("Ungrouped project visibility").check();
  await expect(dialog.getByLabel("Ungrouped project visibility")).toBeChecked();
  const metadataCapability = dialog.getByText("Read metadata", { exact: true }).locator("..").locator("..");
  await metadataCapability.getByRole("button", { name: "Always", exact: true }).click();
  const generateCapability = dialog.getByText("Generate items", { exact: true }).locator("..").locator("..");
  await expect(generateCapability.getByRole("button", { name: "Disabled", exact: true })).toBeVisible();
  await generateCapability.getByRole("button", { name: "Always", exact: true }).click();
  await dialog.getByRole("button", { name: "Save Vault capabilities" }).click();
  await expect(dialog.getByText("Project Vault capabilities saved.")).toBeVisible();
});

for (const { width, height } of responsiveViewportMatrix) {
  test(`@high-risk keeps Vault permission completion reachable at ${width}x${height}`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto("/");
    await page.getByRole("textbox").fill("local-password");
    await page.getByRole("button", { name: "Unlock", exact: true }).click();
    await page.goto("/tokens");

    await page.getByRole("button", { name: "Vault", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "agent Vault permissions" });
    await expect(dialog).toBeVisible();
    const metadataCapability = dialog.getByText("Read metadata", { exact: true }).locator("..").locator("..");
    await metadataCapability.getByRole("button", { name: "Always", exact: true }).click();
    const generateCapability = dialog.getByText("Generate items", { exact: true }).locator("..").locator("..");
    await generateCapability.getByRole("button", { name: "Always", exact: true }).click();
    const save = dialog.getByRole("button", { name: "Save Vault capabilities" });
    await dialog.hover();
    await page.mouse.wheel(0, 3000);
    await expect(save).toBeInViewport();
    await save.click();
    await expect(dialog.getByText("Project Vault capabilities saved.")).toBeVisible();
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  });
}

for (const { width, height } of responsiveViewportMatrix) {
  test(`@high-risk keeps navigation, Console drawers, and permission dialogs usable at ${width}x${height}`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto("/");
    await page.getByRole("textbox").fill("local-password");
    await page.getByRole("button", { name: "Unlock", exact: true }).click();

    if (width < 1024) {
      await page.getByRole("button", { name: "Open navigation" }).click();
      const navigation = page.getByRole("dialog", { name: "Navigation" });
      await expect(navigation).toBeVisible();
      await navigation.getByRole("link", { name: "Console", exact: true }).click();
      await expect(navigation).toBeHidden();
    } else {
      await page.locator('aside a[href="/console"]').click();
    }

    await page.getByRole("button", { name: "Connectors", exact: true }).click();
    const targetsDrawer = page.getByRole("dialog", { name: "Connectors" });
    await expect(targetsDrawer).toBeVisible();
    await expect(targetsDrawer.getByText("worker-1", { exact: true })).toBeVisible();
    await targetsDrawer.getByRole("button", { name: "Close drawer" }).click();

    await page.getByRole("button", { name: "Tokens", exact: true }).click();
    const tokensDrawer = page.getByRole("dialog", { name: "Tokens" });
    await expect(tokensDrawer).toBeVisible();
    await expect(tokensDrawer.getByText("agent", { exact: true })).toBeVisible();
    await tokensDrawer.getByRole("button", { name: "Close drawer" }).click();
    expect(await page.locator("html").evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);

    await page.goto("/tokens");
    expect(await page.locator("html").evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    await page.getByRole("button", { name: "Connectors", exact: true }).click();
    const permissionDialog = page.getByRole("dialog", { name: "agent connector permissions" });
    await expect(permissionDialog).toBeVisible();
    const bounds = await permissionDialog.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    const save = permissionDialog.getByRole("button", { name: "Save connector permissions" });
    await save.scrollIntoViewIfNeeded();
    await expect(save).toBeInViewport();
    expect(await permissionDialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  });
}

test("moves an edited connector to another project", async ({ page }) => {
  let updatePayload = null;
  await page.route("http://localhost:8080/api/connector-targets/1/with-profile/1", async (route) => {
    updatePayload = route.request().postDataJSON();
    await route.fulfill({ json: { ...targetDetail(), project_id: 2, project_name: "My Project", project_slug: "my-project" } });
  });

  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.locator('aside a[href="/connectors"]').click();

  await page.getByTitle("Edit connector").click();
  await page.getByLabel("Project").selectOption("2");
  await page.getByLabel("I will install the key later").check();
  await page.getByRole("button", { name: "Save changes" }).click();

  await expect(page.getByText("Connector updated.")).toBeVisible();
  expect(updatePayload?.target?.project_id).toBe(2);
});

test("@high-risk reviews and runs a Prompt connector action in the selected target context", async ({ page }) => {
  let pending = true;
  let runCount = 0;
  const approval = pendingApproval();
  await page.unroute("http://localhost:8080/api/connector-action-approvals");
  await page.route("http://localhost:8080/api/connector-action-approvals", async (route) => {
    await route.fulfill({ json: pending ? [approval] : [] });
  });
  await page.route("http://localhost:8080/api/connector-action-approvals/42", async (route) => {
    await route.fulfill({ json: approval });
  });
  await page.route("http://localhost:8080/api/connector-action-approvals/42/run", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({ user_note: "" });
    runCount += 1;
    pending = false;
    await route.fulfill({ json: { ...approval, status: "completed" } });
  });

  await unlock(page);
  await page.locator('aside a[href="/console"]').click();

  const dialog = page.getByRole("dialog", { name: "ssh action approval" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Inspect service health", { exact: true })).toBeVisible();
  await expect(dialog.locator("pre").filter({ hasText: '"command": "uptime"' }).first()).toBeVisible();
  await dialog.getByRole("button", { name: "Run", exact: true }).click();

  await expect(dialog).toBeHidden();
  expect(runCount).toBe(1);
});

test("@high-risk keeps structured sessions isolated while switching connector profiles", async ({ page }) => {
  const profiles = [postgresTargetProfile(1, "admin"), postgresTargetProfile(2, "readonly")];
  await page.unroute("http://localhost:8080/api/targets");
  await page.route("http://localhost:8080/api/targets", async (route) => route.fulfill({ json: { items: profiles } }));
  await page.route("http://localhost:8080/api/connector-targets/2/profiles/*/actions", async (route) => {
    await route.fulfill({ json: { items: [postgresQueryAction()] } });
  });
  await page.route("http://localhost:8080/api/connector-actions/local-run", async (route) => {
    await route.fulfill({ json: { request_id: 7, status: "completed", output: { rows: [] } } });
  });

  await unlock(page);
  await page.locator('aside a[href="/console"]').click();
  await expect(page.getByRole("heading", { name: "analytics-db" })).toBeVisible();
  await page.setViewportSize({ width: 1920, height: 1080 });
  const workspaceHeader = page.locator("header").filter({ has: page.getByRole("heading", { name: "analytics-db" }) });
  const profileSelect = workspaceHeader.getByLabel("Profile");
  await expect(profileSelect).toHaveValue("1");
  await expect(workspaceHeader.getByRole("button", { name: "End Session" })).toBeEnabled();

  await profileSelect.selectOption("2");
  await expect(page).toHaveURL(/target=postgres%3A2%3A2/);
  await workspaceHeader.getByRole("button", { name: "End Session" }).click();
  await expect(page.getByRole("heading", { name: "No active Postgres session" })).toBeVisible();

  await profileSelect.selectOption("1");
  await expect(page).toHaveURL(/target=postgres%3A2%3A1/);
  await expect(page.getByRole("heading", { name: "No active Postgres session" })).toBeHidden();
  await profileSelect.selectOption("2");
  await expect(page.getByRole("heading", { name: "No active Postgres session" })).toBeVisible();
});

test("@high-risk reconnects a live console after the remote session exits", async ({ page }) => {
  let socketCount = 0;
  let activeSocket = null;
  let clientSocketReady = false;
  await page.unroute("http://localhost:8080/api/console/sessions");
  await page.route("http://localhost:8080/api/console/sessions", async (route) => {
    if (route.request().method() === "POST") {
      await route.fulfill({ json: liveConsoleSession(11) });
      return;
    }
    await route.fulfill({ json: [liveConsoleSession(10)] });
  });
  await page.route("http://localhost:8080/api/vault-session-options?runtime_id=1", async (route) => {
    await route.fulfill({ json: { supported: false, items: [], defaults: [] } });
  });
  await page.routeWebSocket(/\/api\/console\/sessions\/\d+\/attach/, (socket) => {
    socketCount += 1;
    activeSocket = socket;
    socket.onMessage(() => {
      clientSocketReady = true;
    });
    socket.send(JSON.stringify({ type: "snapshot", status: "connected", data: "ready\r\n" }));
  });

  await unlock(page);
  await page.locator('aside a[href="/console"]').click();
  await expect.poll(() => socketCount).toBe(1);
  await page.getByRole("textbox", { name: "Terminal input" }).press("x");
  await expect.poll(() => clientSocketReady).toBe(true);
  activeSocket.send(JSON.stringify({ type: "exit", status: "closed", data: "Remote shell exited." }));

  await expect(page.getByRole("heading", { name: "No active shell session" })).toBeVisible();
  await expect(page.getByText(/session is closed and cannot accept input anymore/i)).toBeVisible();
  await page.getByRole("button", { name: "New Session", exact: true }).last().click();
  await expect.poll(() => socketCount).toBe(2);
  await expect(page.getByRole("heading", { name: "No active shell session" })).toBeHidden();
});

test("@high-risk cancels an active transfer from the transfer center", async ({ page }) => {
  let canceled = false;
  let cancelCount = 0;
  await page.route("http://localhost:8080/api/file-transfer-batches?limit=30", async (route) => {
    await route.fulfill({ json: { items: [transferBatch(canceled ? "canceled" : "running")] } });
  });
  await page.route("http://localhost:8080/api/file-transfer-batches/77/cancel", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({});
    cancelCount += 1;
    canceled = true;
    await route.fulfill({ json: transferBatch("canceled") });
  });

  await unlock(page);
  await page.getByRole("button", { name: /Transfers/ }).click();
  await expect(page.getByRole("heading", { name: "Transfer Center" })).toBeVisible();
  await expect(page.getByText("1 active queue", { exact: true })).toBeVisible();
  await page.getByTitle("Cancel").click();

  await expect(page.getByText("0 active queues", { exact: true })).toBeVisible();
  await expect(page.getByText("Recent", { exact: true })).toBeVisible();
  expect(cancelCount).toBe(1);
});

async function unlock(page) {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await expect(page.locator('aside a[href="/console"]')).toBeVisible();
}

async function expectNoModerateAccessibilityViolations(page, include) {
  const results = await new AxeBuilder({ page }).include(include).analyze();
  const violations = results.violations.filter(({ impact }) => ["moderate", "serious", "critical"].includes(impact));
  expect(violations, violations.map(({ id, help }) => `${id}: ${help}`).join("\n")).toEqual([]);
}

async function requestFormData(request) {
  const contentType = request.headers()["content-type"];
  expect(contentType).toContain("multipart/form-data");
  const response = new Response(request.postDataBuffer(), { headers: { "content-type": contentType } });
  return response.formData();
}

function pendingApproval() {
  return {
    id: 42,
    connector_kind: "ssh",
    target_name: "worker-1",
    profile_label: "main",
    target_ref: "ssh:1:1",
    token_name: "agent",
    action_name: "exec",
    reason: "Inspect service health",
    input: { command: "uptime" },
    preview: { command: "uptime", mode: "prompt" },
    status: "approval_pending",
    created_at: "2026-09-07T12:00:00Z",
  };
}

function postgresTargetProfile(profileID, label) {
  return {
    ref: `postgres:2:${profileID}`,
    connector_kind: "postgres",
    target_id: 2,
    profile_id: profileID,
    target_name: "analytics-db",
    profile_label: label,
    config: { host: "127.0.0.1", port: 5432, database: "analytics" },
    public: { username: label },
  };
}

function postgresQueryAction() {
  return {
    name: "query_readonly",
    label: "Read query",
    description: "Run a bounded read-only query.",
    category: "query",
    risk: "read",
  };
}

function unlockedStatus() {
  return {
    state: "unlocked",
    database_id: "default",
    database_name: "Default",
    unlocked_databases: [{ id: "default", name: "Default", current: true }],
    databases: [{ id: "default", name: "Default", state: "unlocked" }],
  };
}

function targetSummary() {
  return {
    id: 1,
    project_id: 1,
    project_name: "Ungrouped",
    project_slug: "ungrouped",
    ref: "ssh:1:1",
    connector_kind: "ssh",
    name: "worker-1",
    config: { host: "127.0.0.1", port: 22 },
    status: "active",
  };
}

function project() {
  return {
    id: 1,
    name: "Ungrouped",
    slug: "ungrouped",
    target_count: 1,
    created_at: "2026-05-31T00:00:00Z",
    updated_at: "2026-05-31T00:00:00Z",
  };
}

function secondaryProject() {
  return {
    id: 2,
    name: "My Project",
    slug: "my-project",
    target_count: 0,
    created_at: "2026-05-31T00:00:00Z",
    updated_at: "2026-05-31T00:00:00Z",
  };
}
function projectScope(enabled) {
  return {
    project_id: 1,
    project_name: "Ungrouped",
    project_slug: "ungrouped",
    enabled,
  };
}

function projectCapabilityDefinitions() {
  return [
    {
      name: "vault.metadata.read",
      label: "Read metadata",
      description: "List secret names and bounded non-secret metadata for this project.",
      allowed_rules: ["always_run"],
    },
    {
      name: "vault.item.generate",
      label: "Generate items",
      description: "Generate and store a new secret value without returning it to the agent.",
      allowed_rules: ["approval_required", "always_run"],
    },
    {
      name: "vault.session.apply",
      label: "Apply to sessions",
      description: "Restart an eligible connector session with approved Vault items in its environment.",
      allowed_rules: ["approval_required", "always_run"],
    },
  ];
}

function targetDetail() {
  return {
    ...targetSummary(),
    profiles: [
      {
        id: 1,
        target_id: 1,
        ref: "ssh:1:1",
        connector_kind: "ssh",
        kind: "private_key",
        label: "main",
        public: { username: "root", ssh_key_id: 1 },
      },
    ],
  };
}

function targetInventory() {
  return {
    ...targetSummary(),
    profiles: [
      {
        id: 1,
        target_id: 1,
        ref: "ssh:1:1",
        connector_kind: "ssh",
        kind: "private_key",
        label: "main",
        public: { username: "root", ssh_key_id: 1 },
        actions: [sshExecAction()],
      },
    ],
  };
}

function targetProfile() {
  return {
    ref: "ssh:1:1",
    connector_kind: "ssh",
    target_id: 1,
    profile_id: 1,
    runtime_id: 1,
    target_name: "worker-1",
    profile_label: "main",
    server_id: 1,
    config: { host: "127.0.0.1", port: 22 },
    public: { username: "root", ssh_key_id: 1 },
  };
}

function liveConsoleSession(id) {
  return {
    id,
    runtime_id: 1,
    name: "worker-1 shell",
    status: "connected",
    transcript: "",
    created_at: "2026-09-07T12:00:00Z",
  };
}

function transferBatch(status) {
  return {
    id: 77,
    runtime_id: 1,
    target_name: "worker-1",
    status,
    direction: "download",
    source: "ui",
    total_items: 1,
    completed_items: 0,
    canceled_items: status === "canceled" ? 1 : 0,
    failed_items: 0,
    transferred_bytes: 0,
    items: [],
  };
}

function sshExecAction() {
  return {
    name: "exec",
    label: "Run command",
    description: "Run a non-interactive command.",
    category: "command",
    risk: "write",
  };
}
