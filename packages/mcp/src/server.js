#!/usr/bin/env node

if (process.argv[2] === "init") {
  const { runInit } = await import("./init.js");
  await runInit(process.argv.slice(3));
  process.exit(0);
}

import { readFileSync } from "node:fs";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { callVaultActionSchema, listVaultItemsSchema, vaultActionRequestSchema } from "./vault-tools.js";
import { MCP_SERVER_INSTRUCTIONS } from "./instructions.js";
import { normalizeLocalAPIURL } from "./local-url.js";
import { jsonToolResult } from "./results.js";

const packageMetadata = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
const apiUrl = normalizeLocalAPIURL(process.env.AIPERMISSION_API_URL);
const apiToken = process.env.AIPERMISSION_API_TOKEN || "";
const apiTimeoutMs = Number.parseInt(process.env.AIPERMISSION_HTTP_TIMEOUT_MS || "60000", 10);

const server = new McpServer(
  {
    name: "aipermission",
    version: packageMetadata.version,
  },
  { instructions: MCP_SERVER_INSTRUCTIONS },
);

server.tool(
  "list_connector_targets",
  "List connector targets this AIPermission token can access. Credentials and secrets are never returned.",
  {},
  async () => {
    return jsonToolResult(() => apiGet("/api/mcp/connector-targets"));
  },
);

server.tool(
  "get_connector_help",
  "Read AI-facing help for one connector target/profile. Use this before calling connector actions for the first time.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets in connector:target_id:profile_id format."),
  },
  async ({ target_ref }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams({ target_ref });
      return apiGet(`/api/mcp/connector-help?${params.toString()}`);
    });
  },
);

server.tool(
  "get_connector_actions",
  "List actions exposed by one connector target/profile. Action execution is still checked against token permissions.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets in connector:target_id:profile_id format."),
  },
  async ({ target_ref }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams({ target_ref });
      return apiGet(`/api/mcp/connector-actions?${params.toString()}`);
    });
  },
);

server.tool(
  "call_connector_action",
  "Call one connector action through AIPermission. If status is approval_pending or running, follow assistant_hint and poll get_connector_action_request.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets."),
    action_name: z.string().min(1).describe("Action name from get_connector_actions."),
    input: z.record(z.unknown()).optional().describe("Connector-specific action input."),
    reason: z.string().optional().describe("Why this connector action is needed."),
    idempotency_key: z
      .string()
      .min(1)
      .max(128)
      .optional()
      .describe("Caller-stable key that makes retries return the original request without running twice."),
  },
  async ({ target_ref, action_name, input, reason, idempotency_key }) => {
    return jsonToolResult(() =>
      apiPost("/api/mcp/connector-actions/call", {
        target_ref,
        action_name,
        input: input || {},
        reason: reason || "",
        idempotency_key,
      }),
    );
  },
);

server.tool(
  "get_connector_action_request",
  "Read one connector action request by id. Use this after call_connector_action returns approval_pending or running.",
  {
    request_id: z.number().int().positive().describe("Request id returned by call_connector_action."),
  },
  async ({ request_id }) => {
    return jsonToolResult(() => apiGet(`/api/mcp/connector-action-requests/${request_id}`));
  },
);

server.tool(
  "list_vault_items",
  "List secret names and bounded non-secret Vault metadata for projects this token can read. Secret values are never returned.",
  listVaultItemsSchema,
  async ({ project_ref }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams();
      if (project_ref) params.set("project_ref", project_ref);
      const query = params.toString();
      return apiGet(`/api/mcp/vault-items${query ? `?${query}` : ""}`);
    });
  },
);

server.tool(
  "call_vault_action",
  "Run a Vault action under the configured project capability. Prompt waits for local approval; Always executes immediately through the same tracked request path. generate_item input accepts name, secret_type, generator_kind, provider, environment, description, expires_at, expiry_warning_days, tags (string array), usage_notes (array of {location, notes}), and shared_project_ids (integer array). restart_session_with_environment input requires target_ref and items with item_id, source_project_id, and optional replace_existing. Never include raw secret values.",
  callVaultActionSchema,
  async ({ project_ref, action_name, input, reason, idempotency_key }) => {
    return jsonToolResult(() =>
      apiPost("/api/mcp/vault-actions/call", {
        project_ref,
        action_name,
        input,
        reason,
        idempotency_key,
      }),
    );
  },
);

server.tool(
  "get_vault_action_request",
  "Read one Vault action request after call_vault_action returns approval_pending. Responses never include secret values.",
  vaultActionRequestSchema,
  async ({ request_id }) => {
    return jsonToolResult(() => apiGet(`/api/mcp/vault-action-requests/${request_id}`));
  },
);

server.tool(
  "cancel_vault_action_request",
  "Cancel one approval_pending Vault action request owned by this token. Running or terminal requests cannot be canceled.",
  vaultActionRequestSchema,
  async ({ request_id }) => {
    return jsonToolResult(() => apiPost(`/api/mcp/vault-action-requests/${request_id}/cancel`, {}));
  },
);

const transport = new StdioServerTransport();
await server.connect(transport);

async function apiGet(path) {
  return apiRequest(path, {
    method: "GET",
  });
}

async function apiPost(path, body) {
  return apiRequest(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

async function apiRequest(path, options) {
  const response = await apiFetch(path, options);
  const text = await response.text();
  const data = parseResponseBody(text);
  if (!response.ok) {
    const error = new Error(data?.error || `AIPermission API request failed with ${response.status}`);
    if (data?.code) error.code = data.code;
    throw error;
  }
  return data;
}

async function apiFetch(path, options) {
  if (!apiToken) {
    throw new Error("AIPERMISSION_API_TOKEN is required.");
  }
  const timeout = Number.isFinite(apiTimeoutMs) && apiTimeoutMs > 0 ? apiTimeoutMs : 60000;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);
  let response;
  try {
    response = await fetch(`${apiUrl}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        Authorization: `Bearer ${apiToken}`,
        ...(options.headers || {}),
      },
    });
  } catch (error) {
    if (error?.name === "AbortError") {
      throw new Error(`AIPermission API request timed out after ${timeout}ms`, { cause: error });
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
  return response;
}

function parseResponseBody(text) {
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    return { error: text };
  }
}
