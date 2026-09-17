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
import { gatewayAPIError } from "./api-error.js";
import { parseHTTPTimeout } from "./config.js";
import { idempotencyKeySchema } from "./idempotency-key.js";
import { normalizeLocalAPIURL } from "./local-url.js";
import { projectGatewaySuccess, responseContracts } from "./response-contracts.js";
import { jsonToolResult } from "./results.js";

const packageMetadata = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
let apiUrl = "";
let apiURLConfigurationError = null;
try {
  apiUrl = normalizeLocalAPIURL(process.env.AIPERMISSION_API_URL);
} catch (error) {
  apiURLConfigurationError = new Error("Invalid local gateway URL configuration; update AIPERMISSION_API_URL.", { cause: error });
}
const apiToken = process.env.AIPERMISSION_API_TOKEN || "";
const apiTimeoutMs = parseHTTPTimeout(process.env.AIPERMISSION_HTTP_TIMEOUT_MS);

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
  async (_args, { signal }) => {
    return jsonToolResult(
      () => apiGet("/api/mcp/connector-targets", { signal }),
      (value) => projectGatewaySuccess(responseContracts.connectorTargets, value),
    );
  },
);

server.tool(
  "get_connector_help",
  "Read AI-facing help for one connector target/profile. Use this before calling connector actions for the first time.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets in connector:target_id:profile_id format."),
  },
  async ({ target_ref }, { signal }) => {
    return jsonToolResult(
      () => {
        const params = new URLSearchParams({ target_ref });
        return apiGet(`/api/mcp/connector-help?${params.toString()}`, { signal });
      },
      (value) => projectGatewaySuccess(responseContracts.connectorHelp, value),
    );
  },
);

server.tool(
  "get_connector_actions",
  "List actions exposed by one connector target/profile. Action execution is still checked against token permissions.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets in connector:target_id:profile_id format."),
  },
  async ({ target_ref }, { signal }) => {
    return jsonToolResult(
      () => {
        const params = new URLSearchParams({ target_ref });
        return apiGet(`/api/mcp/connector-actions?${params.toString()}`, { signal });
      },
      (value) => projectGatewaySuccess(responseContracts.connectorActions, value),
    );
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
    idempotency_key: idempotencyKeySchema,
  },
  async ({ target_ref, action_name, input, reason, idempotency_key }, { signal }) => {
    return jsonToolResult(
      () =>
        apiPost(
          "/api/mcp/connector-actions/call",
          {
            target_ref,
            action_name,
            input: input || {},
            reason: reason || "",
            idempotency_key,
          },
          { signal },
        ),
      (value) => projectGatewaySuccess(responseContracts.connectorAction, value),
      { idempotencyKey: idempotency_key },
    );
  },
);

server.tool(
  "get_connector_action_request",
  "Read one connector action request by id. Use this after call_connector_action returns approval_pending or running.",
  {
    request_id: z.number().int().positive().describe("Request id returned by call_connector_action."),
  },
  async ({ request_id }, { signal }) => {
    return jsonToolResult(
      () => apiGet(`/api/mcp/connector-action-requests/${request_id}`, { signal }),
      (value) => projectGatewaySuccess(responseContracts.connectorAction, value),
    );
  },
);

server.tool(
  "list_vault_items",
  "List secret names and bounded non-secret Vault metadata for projects this token can read. Secret values are never returned.",
  listVaultItemsSchema,
  async ({ project_ref }, { signal }) => {
    return jsonToolResult(
      () => {
        const params = new URLSearchParams();
        if (project_ref) params.set("project_ref", project_ref);
        const query = params.toString();
        return apiGet(`/api/mcp/vault-items${query ? `?${query}` : ""}`, { signal });
      },
      (value) => projectGatewaySuccess(responseContracts.vaultItems, value),
    );
  },
);

server.tool(
  "call_vault_action",
  "Run a Vault action under the configured project capability. Prompt waits for local approval; Always executes immediately through the same tracked request path. generate_item input accepts name, secret_type, generator_kind, provider, environment, description, expires_at, expiry_warning_days, tags (string array), usage_notes (array of {location, notes}), and shared_project_ids (integer array). restart_session_with_environment input requires target_ref and items with item_id, source_project_id, and optional replace_existing. Never include raw secret values.",
  callVaultActionSchema,
  async ({ project_ref, action_name, input, reason, idempotency_key }, { signal }) => {
    return jsonToolResult(
      () =>
        apiPost(
          "/api/mcp/vault-actions/call",
          {
            project_ref,
            action_name,
            input,
            reason,
            idempotency_key,
          },
          { signal },
        ),
      (value) => projectGatewaySuccess(responseContracts.vaultAction, value),
      { idempotencyKey: idempotency_key },
    );
  },
);

server.tool(
  "get_vault_action_request",
  "Read one Vault action request after call_vault_action returns approval_pending. Responses never include secret values.",
  vaultActionRequestSchema,
  async ({ request_id }, { signal }) => {
    return jsonToolResult(
      () => apiGet(`/api/mcp/vault-action-requests/${request_id}`, { signal }),
      (value) => projectGatewaySuccess(responseContracts.vaultAction, value),
    );
  },
);

server.tool(
  "cancel_vault_action_request",
  "Cancel one approval_pending Vault action request owned by this token. Running or terminal requests cannot be canceled.",
  vaultActionRequestSchema,
  async ({ request_id }, { signal }) => {
    return jsonToolResult(
      () => apiPost(`/api/mcp/vault-action-requests/${request_id}/cancel`, {}, { requestID: request_id, signal }),
      (value) => projectGatewaySuccess(responseContracts.vaultAction, value),
      { requestID: request_id },
    );
  },
);

const transport = new StdioServerTransport();
await server.connect(transport);

async function apiGet(path, context = {}) {
  return apiRequest(
    path,
    {
      method: "GET",
    },
    undefined,
    context,
  );
}

async function apiPost(path, body, context = {}) {
  return apiRequest(
    path,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    },
    body?.idempotency_key,
    context,
  );
}

async function apiRequest(path, options, idempotencyKey, context = {}) {
  if (apiURLConfigurationError) {
    throw apiURLConfigurationError;
  }
  if (!apiToken) {
    throw new Error("AIPERMISSION_API_TOKEN is required.");
  }
  let request;
  try {
    request = new Request(`${apiUrl}${path}`, {
      ...options,
      headers: new Headers({ Authorization: `Bearer ${apiToken}`, ...(options.headers || {}) }),
    });
  } catch {
    throw new Error("Invalid local gateway request configuration; check the API URL and token.");
  }
  const timeout = apiTimeoutMs;
  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeout);
  const cancelRequest = () => controller.abort();
  context.signal?.addEventListener("abort", cancelRequest, { once: true });
  let bodyReceived = false;
  let dispatchStarted = false;
  try {
    if (context.signal?.aborted) {
      throw new Error("MCP request was canceled.");
    }
    dispatchStarted = true;
    const response = await fetch(request, { signal: controller.signal });
    const text = await response.text();
    const data = response.status === 204 ? null : parseResponseBody(text);
    bodyReceived = true;
    if (!response.ok) {
      throw gatewayAPIError(data, response.status, response.headers.get("Retry-After"));
    }
    return data;
  } catch (error) {
    let failure = bodyReceived ? error : new Error("Gateway response unavailable or invalid.", { cause: error });
    if (timedOut) {
      failure = new Error(`AIPermission API request timed out after ${timeout}ms`, { cause: error });
    } else if (context.signal?.aborted) {
      failure = new Error("MCP request was canceled.", { cause: error });
    }
    if (!dispatchStarted) throw failure;
    const definitelyNotDispatched = !bodyReceived && isDefinitePredispatchTransportError(error);
    if (definitelyNotDispatched) {
      throw new Error("AIPermission gateway connection failed before request dispatch.", { cause: error });
    }
    if (options.method === "POST" && !bodyReceived) {
      const uncertain = new Error(failure.message || "Gateway response unavailable", { cause: failure });
      uncertain.resultStatus = "outcome_unknown";
      uncertain.code = "gateway_transport_outcome_unknown";
      uncertain.idempotencyKey = idempotencyKey;
      if (Number.isSafeInteger(context.requestID) && context.requestID > 0) uncertain.requestID = context.requestID;
      uncertain.assistantHint = idempotencyKey
        ? "The gateway response was lost; execution may have occurred. Reconcile the original request using the same idempotency key and unchanged input. Never retry with a new key blindly."
        : "The gateway response was lost; execution may have occurred. Inspect the original request status before repeating the operation.";
      throw uncertain;
    }
    throw failure;
  } finally {
    clearTimeout(timer);
    context.signal?.removeEventListener("abort", cancelRequest);
  }
}

const definitePredispatchErrorCodes = new Set([
  "ECONNREFUSED",
  "ENETUNREACH",
  "EHOSTUNREACH",
  "ENOTFOUND",
  "EAI_AGAIN",
  "UND_ERR_CONNECT_TIMEOUT",
  "ERR_TLS_CERT_ALTNAME_INVALID",
  "DEPTH_ZERO_SELF_SIGNED_CERT",
  "SELF_SIGNED_CERT_IN_CHAIN",
  "UNABLE_TO_VERIFY_LEAF_SIGNATURE",
  "CERT_HAS_EXPIRED",
]);

function isDefinitePredispatchTransportError(error) {
  const pending = [error];
  const visited = new Set();
  while (pending.length > 0) {
    const current = pending.pop();
    if (!current || (typeof current !== "object" && typeof current !== "function") || visited.has(current)) continue;
    visited.add(current);
    if (definitePredispatchErrorCodes.has(current.code)) return true;
    if (current.cause) pending.push(current.cause);
    if (Array.isArray(current.errors)) pending.push(...current.errors);
  }
  return false;
}

function parseResponseBody(text) {
  try {
    const data = JSON.parse(text);
    if (!data || typeof data !== "object") throw new Error("Invalid response shape");
    return data;
  } catch {
    throw new Error("Gateway returned an invalid JSON response.");
  }
}
