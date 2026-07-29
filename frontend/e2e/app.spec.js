import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  let unlocked = false;
  let connectorPermissions = [];
  let projectCapabilities = [];
  let enabledProjectIDs = [1];
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
    unlocked = true;
    await route.fulfill({
      headers: {
        "set-cookie": "aipermission_ui_session=test; Path=/; SameSite=Strict",
      },
      json: { state: "unlocked", database_id: "default", database_name: "Default" },
    });
  });
  await page.route("http://localhost:8080/api/backup/import", async (route) => {
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
      await route.fulfill({ json: { reusable_tokens: false, expose_mcp_server_metadata: true, mcp_start_enabled: false, redaction_mode: "basic" } });
      return;
    }
    await route.fulfill({ json: { reusable_tokens: false, expose_mcp_server_metadata: false, mcp_start_enabled: false, redaction_mode: "basic" } });
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
  await page.route("http://localhost:8080/api/tokens/1/connector-permissions", async (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      connectorPermissions = body.permissions || [];
      await route.fulfill({ json: { items: connectorPermissions } });
      return;
    }
    await route.fulfill({ json: { items: connectorPermissions } });
  });
  await page.route("http://localhost:8080/api/tokens/1/project-scopes", async (route) => {
    if (route.request().method() === "PUT") {
      enabledProjectIDs = route.request().postDataJSON().enabled_project_ids || [];
    }
    await route.fulfill({ json: { items: [projectScope(enabledProjectIDs.includes(1))] } });
  });
  await page.route("http://localhost:8080/api/tokens/1/project-capabilities", async (route) => {
    if (route.request().method() === "PUT") {
      projectCapabilities = (route.request().postDataJSON().capabilities || []).map((capability) => ({
        ...capability,
        token_id: 1,
        project_name: "Ungrouped",
        project_slug: "ungrouped",
        project_enabled: enabledProjectIDs.includes(capability.project_id),
        revision: 1,
      }));
    }
    await route.fulfill({ json: { definitions: projectCapabilityDefinitions(), items: projectCapabilities } });
  });
});

test("unlocks the local UI session and renders the dashboard", async ({ page }) => {
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

test("imports a database from the unlock screen", async ({ page }) => {
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
  await page.getByLabel("Command history days").fill("14");
  await page.getByLabel("Audit log days").fill("14");
  await page.getByRole("button", { name: "Save retention" }).click();
  await expect(page.getByText("Retention settings saved and cleanup ran.")).toBeVisible();
});

test("updates token connector permission from the Tokens page", async ({ page }) => {
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

test("updates project Vault permissions from the Tokens page", async ({ page }) => {
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
    target_name: "worker-1",
    profile_label: "main",
    server_id: 1,
    config: { host: "127.0.0.1", port: 22 },
    public: { username: "root", ssh_key_id: 1 },
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
