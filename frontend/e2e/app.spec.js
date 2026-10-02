import { expect } from "@playwright/test";
import { createServer } from "node:http";
import { test, mockResponse, installStaticMockRoutes } from "./mock-browser.mjs";
import AxeBuilder from "@axe-core/playwright";
import { responsiveViewportMatrix } from "../scripts/playwright-gate-manifest.mjs";
import { observeSQLBrowserRuntime, verifySQLBrowserRuntime } from "./sql-editor-browser.mjs";
import { scopedUICookieName } from "../src/lib/ui-cookie";
import { databaseName, reconciliationsStore } from "../src/lib/local-action-retry/constants";
import { verifySSHCleanupBrowser } from "./ssh-cleanup-browser.mjs";

test.beforeEach(async ({ page, apiIsolation }) => {
  await installStaticMockRoutes(page, apiIsolation);
  let unlocked = false;
  let connectorPermissions = [];
  let connectorPermissionRevision = "connector-permissions-1";
  let projectCapabilities = [];
  let projectCapabilityRevision = "project-capabilities-1";
  let enabledProjectIDs = [1];
  let projectScopeRevision = "project-scopes-1";
  let mcpRuntimeEnabled = false;
  await apiIsolation.route(page, "http://localhost:8080/api/unlock/status", ["GET"], async (route) => {
    await route.fulfill({
      headers: unlocked
        ? { "X-AIPermission-Workspace": "browser-fixture-workspace", "access-control-expose-headers": "X-AIPermission-Workspace" }
        : {},
      json: unlocked
        ? mockResponse("unlockedStatus")
        : {
            state: "session_required",
            database_id: "default",
            databases: [{ id: "default", name: "Default", state: "locked", unlocked: false }],
          },
    });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/unlock", ["POST"], async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({ database_id: "default", password: "local-password" });
    unlocked = true;
    await route.fulfill({
      headers: {
        "set-cookie": "aipermission_ui_session=test; Path=/; SameSite=Strict",
        "X-AIPermission-Workspace": "browser-fixture-workspace",
        "access-control-expose-headers": "X-AIPermission-Workspace",
      },
      json: { state: "unlocked", database_id: "default", database_name: "Default" },
    });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/backup/import", ["POST"], async (route) => {
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

  await apiIsolation.route(page, "http://localhost:8080/api/settings/security", ["GET", "PUT"], async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({
        json: {
          reusable_tokens: false,
          expose_mcp_server_metadata: true,
          mcp_start_enabled: false,
          redaction_mode: "basic",
          revision: "security-2",
        },
      });
      return;
    }
    await route.fulfill({
      json: {
        reusable_tokens: false,
        expose_mcp_server_metadata: false,
        mcp_start_enabled: false,
        redaction_mode: "basic",
        revision: "security-1",
      },
    });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/settings/mcp-runtime", ["GET", "PUT"], async (route) => {
    if (route.request().method() === "PUT") {
      mcpRuntimeEnabled = Boolean(route.request().postDataJSON().enabled);
    }
    await route.fulfill({ json: { enabled: mcpRuntimeEnabled, start_enabled: false, updated_at: "2026-05-31T00:00:00Z" } });
  });

  await apiIsolation.route(page, "http://localhost:8080/api/settings/retention", ["GET", "PUT"], async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({ json: { history_days: 14, audit_days: 14, console_days: 7, message_days: 7 } });
      return;
    }
    await route.fulfill({ json: { history_days: 0, audit_days: 0, console_days: 0, message_days: 0 } });
  });

  await apiIsolation.route(page, /http:\/\/localhost:8080\/api\/history\?.*/, ["GET"], async (route) => {
    await route.fulfill({ json: { items: [], total: 0, limit: 50, has_more: false } });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/tokens/1/connector-permissions", ["GET", "PUT"], async (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      expect(body).toEqual({
        permissions: [{ target_id: 1, profile_id: 1, action_name: "exec", execution_rule: "approval_required" }],
        expected_revision: connectorPermissionRevision,
      });
      connectorPermissions = body.permissions.map((permission) => ({
        project_id: 1,
        project_name: "Ungrouped",
        project_slug: "ungrouped",
        project_enabled: true,
        target_name: "worker-1",
        profile_label: "main",
        target_ref: "ssh:1:1",
        connector_kind: "ssh",
        profile_kind: "private_key",
        created_at: "2026-05-31T00:00:00Z",
        updated_at: "2026-05-31T00:00:00Z",
        ...permission,
      }));
      connectorPermissionRevision = "connector-permissions-2";
      await route.fulfill({ json: { items: connectorPermissions, revision: connectorPermissionRevision } });
      return;
    }
    expect(route.request().method()).toBe("GET");
    await route.fulfill({ json: { items: connectorPermissions, revision: connectorPermissionRevision } });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/tokens/1/project-scopes", ["GET", "PUT"], async (route) => {
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
  await apiIsolation.route(page, "http://localhost:8080/api/tokens/1/project-capabilities", ["GET", "PUT"], async (route) => {
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
      json: { definitions: mockResponse("projectCapabilityDefinitions"), items: projectCapabilities, revision: projectCapabilityRevision },
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

test("renders security settings and updates MCP metadata exposure", async ({ page, apiIsolation }) => {
  await page.goto("/");
  await page.getByRole("textbox").fill("local-password");
  await page.getByRole("button", { name: "Unlock", exact: true }).click();
  await page.getByRole("link", { name: /Security/ }).click();

  await expect(page.getByRole("heading", { name: "Security" })).toBeVisible();
  await expect(page.getByText("MCP connector targets hide endpoint inventory details by default.")).toBeVisible();
  await page.getByLabel("Expose endpoint metadata to MCP").click();
  await expect(page.getByText("MCP connector targets now include endpoint metadata.")).toBeVisible();
  await verifyAPIIsolation(page, apiIsolation);
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

test("@high-risk persists an explicit server-only reconciliation across reload without marking execution successful", async ({
  page,
  context,
  apiIsolation,
}) => {
  const request = { ...mockResponse("pendingApproval"), status: "outcome_unknown" };
  const headers = {
    "X-AIPermission-Workspace": "browser-fixture-workspace",
    "access-control-expose-headers": "X-AIPermission-Workspace",
  };
  let verifiedReads = 0;
  await apiIsolation.route(page, "http://localhost:8080/api/connector-action-approvals?status=outcome_unknown", ["GET"], async (route) => {
    expect(route.request().method()).toBe("GET");
    expect(route.request().headers()["x-aipermission-workspace"]).toBe(headers["X-AIPermission-Workspace"]);
    await route.fulfill({ headers, json: [request] });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/connector-action-approvals/42", ["GET"], async (route) => {
    expect(route.request().method()).toBe("GET");
    expect(route.request().headers()["x-aipermission-workspace"]).toBe(headers["X-AIPermission-Workspace"]);
    verifiedReads += 1;
    await route.fulfill({ headers, json: request });
  });
  await unlock(page);
  await context.addCookies([
    {
      name: scopedUICookieName("aipermission_workspace", new URL(page.url())),
      value: headers["X-AIPermission-Workspace"],
      url: new URL(page.url()).origin,
      sameSite: "Strict",
    },
  ]);
  await page.getByRole("link", { name: /Settings/ }).click();
  await page.getByRole("button", { name: "Reconcile server request 42" }).click();
  expect(verifiedReads).toBe(0);
  await page.getByRole("dialog").getByRole("button", { name: "Reconcile server request", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Reconcile server request 42" })).toHaveCount(0);
  expect(verifiedReads).toBe(1);
  const proof = {
    scope: headers["X-AIPermission-Workspace"],
    request_id: 42,
    target_ref: request.target_ref,
    action_name: request.action_name,
  };
  expect(await readReconciliationProofs(page)).toMatchObject([proof]);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Unresolved server requests" })).toBeVisible();
  expect(await readReconciliationProofs(page)).toMatchObject([proof]);
  await expect(page.getByRole("button", { name: "Reconcile server request 42" })).toHaveCount(0);
  expect(request.status).toBe("outcome_unknown");
  expect(verifiedReads).toBe(1);
});

async function readReconciliationProofs(page) {
  return page.evaluate(
    ({ name, store }) =>
      new Promise((resolve, reject) => {
        const open = indexedDB.open(name);
        open.onerror = () => reject(open.error);
        open.onsuccess = () => {
          const database = open.result;
          const read = database.transaction(store, "readonly").objectStore(store).getAll();
          read.onsuccess = () => {
            database.close();
            resolve(read.result);
          };
          read.onerror = () => {
            database.close();
            reject(read.error);
          };
        };
      }),
    { name: databaseName, store: reconciliationsStore },
  );
}

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
    await expect.poll(() => new URL(page.url()).pathname).toBe(path);
    await expectNoModerateAccessibilityViolations(page, "main");
  }
});

for (const width of [320, 360]) {
  test(`@accessibility keeps project tables and connector/Vault dialogs usable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/");
    await page.getByRole("textbox").fill("local-password");
    await page.getByRole("button", { name: "Unlock", exact: true }).click();
    await expect(page.getByRole("button", { name: "Open navigation" })).toBeVisible();
    await page.goto("/projects");
    const tableViewport = page.locator("main .overflow-x-auto").filter({ has: page.getByRole("table") });
    await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
    await expect(page.getByRole("table")).toBeVisible();
    expect(await tableViewport.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true);
    await tableViewport.evaluate((element) => {
      element.scrollLeft = element.scrollWidth;
    });
    await expect(page.getByTitle("Archive project").last()).toBeInViewport();
    await expectNoModerateAccessibilityViolations(page, "main");

    await page.goto("/tokens");
    await page.getByRole("button", { name: "Connectors" }).click();
    await expectNoModerateAccessibilityViolations(page, "[role=dialog]");
    await page.getByRole("dialog").getByRole("button", { name: "Close dialog" }).click();
    await page.getByRole("button", { name: "Vault", exact: true }).click();
    await expectNoModerateAccessibilityViolations(page, "[role=dialog]");
  });
}

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

test("moves an edited connector to another project", async ({ page, apiIsolation }) => {
  let updatePayload = null;
  await apiIsolation.route(page, "http://localhost:8080/api/connector-targets/1/with-profile/1", ["PUT"], async (route) => {
    updatePayload = route.request().postDataJSON();
    await route.fulfill({
      json: { ...mockResponse("targetDetail"), project_id: 2, project_name: "My Project", project_slug: "my-project" },
    });
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

test("@high-risk reviews and runs a Prompt connector action in the selected target context", async ({ page, apiIsolation }) => {
  let pending = true;
  let runCount = 0;
  const approval = mockResponse("pendingApproval");
  await page.unroute("http://localhost:8080/api/connector-action-approvals");
  await apiIsolation.route(page, "http://localhost:8080/api/connector-action-approvals", ["GET"], async (route) => {
    await route.fulfill({ json: pending ? [approval] : [] });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/connector-action-approvals/42", ["GET"], async (route) => {
    await route.fulfill({ json: approval });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/connector-action-approvals/42/run", ["POST"], async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({ user_note: "", approval_context_hash: "approval-context-42" });
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

test("@high-risk keeps structured sessions isolated while switching connector profiles", async ({ page, apiIsolation }, testInfo) => {
  const browserErrors = await observeSQLBrowserRuntime(page);
  for (const [kind, label] of [
    ["postgres", "Postgres"],
    ["clickhouse", "ClickHouse"],
  ]) {
    const manualQueries = [];
    const profiles = [sqlTargetProfile(kind, 1, "admin"), sqlTargetProfile(kind, 2, "readonly")];
    await page.unroute("http://localhost:8080/api/targets");
    await apiIsolation.route(page, "http://localhost:8080/api/targets", ["GET"], async (route) =>
      route.fulfill({ json: { items: profiles } }),
    );
    await page.unroute("http://localhost:8080/api/connector-action-approvals");
    await apiIsolation.route(page, "http://localhost:8080/api/connector-action-approvals", ["GET"], async (route) => {
      await route.fulfill({
        json: [
          {
            ...mockResponse("pendingApproval"),
            connector_kind: kind,
            target_id: 2,
            target_name: "analytics-db",
            target_ref: `${kind}:2:1`,
            profile_label: "admin",
            action_name: "query_readonly",
            reason: `manual ${label} console query`,
            input: { sql: "SELECT 2", max_rows: 100 },
            preview: { sql: "SELECT 2", max_rows: 100 },
            status: "completed",
            retry_policy: { class: "read_only", guidance: "Read-only fixture." },
          },
        ],
      });
    });
    await apiIsolation.route(page, "http://localhost:8080/api/connector-targets/2/profiles/*/actions", ["GET"], async (route) => {
      await route.fulfill({ json: { items: [mockResponse("sqlQueryAction")] } });
    });
    await apiIsolation.route(page, "http://localhost:8080/api/connector-actions/local-run", ["POST"], async (route) => {
      expect(route.request().method()).toBe("POST");
      const request = route.request().postDataJSON();
      if (request.reason === `manual ${label} console query`) {
        expect(request).toEqual({
          idempotency_key: expect.any(String),
          target_ref: `${kind}:2:1`,
          action_name: "query_readonly",
          input: { sql: "SELECT 1", max_rows: 100 },
          reason: `manual ${label} console query`,
        });
        manualQueries.push(request);
      }
      await route.fulfill({
        json: {
          request_id: 7,
          target_ref: request.target_ref,
          connector_kind: kind,
          action_name: request.action_name,
          status: "completed",
          retry_policy: { class: "read_only", guidance: "Read-only fixture." },
          output: { rows: [] },
        },
      });
    });

    if (kind === "postgres") await unlock(page);
    else await page.goto("/");
    await page.locator('aside a[href="/console"]').click();
    await expect(page.getByRole("heading", { name: "analytics-db" })).toBeVisible();
    await page.setViewportSize({ width: 1920, height: 1080 });
    const workspaceHeader = page.locator("header").filter({ has: page.getByRole("heading", { name: "analytics-db" }) });
    const profileSelect = workspaceHeader.getByLabel("Profile");
    await expect(profileSelect).toHaveValue("1");
    await expect(workspaceHeader.getByRole("button", { name: "End Session" })).toBeEnabled();
    await verifySQLBrowserRuntime(page, testInfo, manualQueries, browserErrors, kind);

    await profileSelect.selectOption("2");
    await expect(page).toHaveURL(new RegExp(`target=${kind}%3A2%3A2`));
    await workspaceHeader.getByRole("button", { name: "End Session" }).click();
    await expect(page.getByRole("heading", { name: `No active ${label} session` })).toBeVisible();

    await profileSelect.selectOption("1");
    await expect(page).toHaveURL(new RegExp(`target=${kind}%3A2%3A1`));
    await expect(page.getByRole("heading", { name: `No active ${label} session` })).toBeHidden();
    await profileSelect.selectOption("2");
    await expect(page.getByRole("heading", { name: `No active ${label} session` })).toBeVisible();
  }
});

test("@high-risk reconnects a live console after the remote session exits", async ({ page, apiIsolation }) => {
  let socketCount = 0;
  let activeSocket = null;
  let clientSocketReady = false;
  await page.unroute("http://localhost:8080/api/console/sessions");
  await apiIsolation.route(page, "http://localhost:8080/api/console/sessions", ["GET", "POST"], async (route) => {
    if (route.request().method() === "POST") {
      await route.fulfill({ json: liveConsoleSession(11) });
      return;
    }
    await route.fulfill({ json: [liveConsoleSession(10)] });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/vault-session-options?runtime_id=1", ["GET"], async (route) => {
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

test("@high-risk cancels an active transfer from the transfer center", async ({ page, apiIsolation }) => {
  let canceled = false;
  let cancelCount = 0;
  await apiIsolation.route(page, "http://localhost:8080/api/file-transfer-batches?limit=30", ["GET"], async (route) => {
    await route.fulfill({ json: { items: [transferBatch(canceled ? "canceled" : "running")] } });
  });
  await apiIsolation.route(page, "http://localhost:8080/api/file-transfer-batches/77/cancel", ["POST"], async (route) => {
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

for (const width of [390, 1280]) {
  test(`@high-risk reconciles SSH cleanup without remote execution at ${width}px`, async ({ page }, testInfo) => {
    await verifySSHCleanupBrowser({ page, testInfo, width, unlock, expectNoModerateAccessibilityViolations });
  });
}

async function verifyAPIIsolation(page, apiIsolation) {
  const copy = mockResponse("pendingApproval");
  copy.input.command = "changed in this test";
  expect(mockResponse("pendingApproval").input.command).toBe("uptime");
  expect(apiIsolation.unexpectedCalls).toEqual([]);
  const wireCalls = [];
  const server = createServer((request, response) => {
    wireCalls.push(`${request.method} ${request.url}`);
    response.setHeader("Access-Control-Allow-Origin", "*");
    response.end("sentinel");
  });
  server.on("upgrade", (request, socket) => {
    wireCalls.push(`WEBSOCKET ${request.url}`);
    socket.destroy();
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const origin = `http://127.0.0.1:${server.address().port}`;
  try {
    expect(await (await fetch(`${origin}/health`)).text()).toBe("sentinel");
    expect(wireCalls.splice(0)).toEqual(["GET /health"]);
    const probes = [
      { method: "GET", url: `${origin}/api/not-fixtured?probe=read` },
      { method: "POST", url: `${origin}/api/not-fixtured/` },
      { method: "GET", url: `${origin}/%61pi/not-fixtured` },
      { method: "GET", url: "http://localhost:8080/api/status?unexpected=1" },
      { method: "POST", url: "http://localhost:8080/api/status" },
    ];
    const results = await page.evaluate(
      async (requests) =>
        Promise.all(
          requests.map(async ({ method, url }) => {
            try {
              await fetch(url, { method, body: method === "POST" ? "probe" : undefined });
              return "allowed";
            } catch {
              return "blocked";
            }
          }),
        ),
      probes,
    );
    expect(results).toEqual(["blocked", "blocked", "blocked", "blocked", "blocked"]);
    const websocketURL = `${origin.replace("http:", "ws:")}/api/not-fixtured/socket`;
    expect(
      await page.evaluate(
        (url) =>
          new Promise((resolve) => {
            const socket = new WebSocket(url);
            socket.onclose = (event) => resolve(event.code);
          }),
        websocketURL,
      ),
    ).toBe(1008);
    expect(wireCalls).toEqual([]);
    expect(apiIsolation.unexpectedCalls.toSorted()).toEqual(
      [...probes.map(({ method, url }) => `${method} ${url}`), `WEBSOCKET ${websocketURL}`].sort(),
    );
    expect(() => apiIsolation.assertClean()).toThrow(/Unexpected mock API calls/);
    // Consume only the exact intentional probes; any unrelated call still fails teardown.
    apiIsolation.unexpectedCalls.splice(0, probes.length + 1);
    apiIsolation.assertClean();
  } finally {
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
}

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

function projectScope(enabled) {
  return { ...mockResponse("projectScope"), enabled };
}

function sqlTargetProfile(kind, profileID, label) {
  const profile = mockResponse("sqlTargetProfile");
  return {
    ...profile,
    ref: `${kind}:2:${profileID}`,
    connector_kind: kind,
    profile_id: profileID,
    profile_label: label,
    config: { ...profile.config, port: kind === "postgres" ? 5432 : 9000 },
    public: { username: label },
  };
}

function liveConsoleSession(id) {
  return { ...mockResponse("liveConsoleSession"), id };
}

function transferBatch(status) {
  return { ...mockResponse("transferBatch"), status, canceled_items: status === "canceled" ? 1 : 0 };
}
