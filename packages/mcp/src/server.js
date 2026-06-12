#!/usr/bin/env node

if (process.argv[2] === "init") {
  const { runInit } = await import("./init.js");
  await runInit(process.argv.slice(3));
  process.exit(0);
}

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { normalizeLocalAPIURL } from "./local-url.js";
import { jsonToolResult } from "./results.js";

const apiUrl = normalizeLocalAPIURL(process.env.AIPERMISSION_API_URL);
const apiToken = process.env.AIPERMISSION_API_TOKEN || "";
const apiTimeoutMs = Number.parseInt(process.env.AIPERMISSION_HTTP_TIMEOUT_MS || "60000", 10);

const server = new McpServer({
  name: "aipermission",
  version: "0.1.14",
});

server.tool(
  "list_connector_targets",
  "List connector targets this AIPermission token can access. Credentials and secrets are never returned.",
  {},
  async () => {
    return jsonToolResult(() => apiGet("/api/mcp/connector-targets"));
  }
);

server.tool(
  "get_connector_help",
  "Read AI-facing help for one connector target/profile. Use this before calling connector actions for the first time.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets, such as ssh:12:3 or postgres:7:11."),
  },
  async ({ target_ref }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams({ target_ref });
      return apiGet(`/api/mcp/connector-help?${params.toString()}`);
    });
  }
);

server.tool(
  "get_connector_actions",
  "List actions exposed by one connector target/profile. Action execution is still checked against token permissions.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets, such as ssh:12:3 or postgres:7:11."),
  },
  async ({ target_ref }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams({ target_ref });
      return apiGet(`/api/mcp/connector-actions?${params.toString()}`);
    });
  }
);

server.tool(
  "call_connector_action",
  "Call one connector action through AIPermission. If status is approval_pending or running, follow assistant_hint and poll get_connector_action_request.",
  {
    target_ref: z.string().min(1).describe("Target ref from list_connector_targets."),
    action_name: z.string().min(1).describe("Action name from get_connector_actions."),
    input: z.record(z.unknown()).optional().describe("Connector-specific action input."),
    reason: z.string().optional().describe("Why this connector action is needed."),
  },
  async ({ target_ref, action_name, input, reason }) => {
    return jsonToolResult(() => apiPost("/api/mcp/connector-actions/call", {
      target_ref,
      action_name,
      input: input || {},
      reason: reason || "",
    }));
  }
);

server.tool(
  "get_connector_action_request",
  "Read one connector action request by id. Use this after call_connector_action returns approval_pending or running.",
  {
    request_id: z.number().int().positive().describe("Request id returned by call_connector_action."),
  },
  async ({ request_id }) => {
    return jsonToolResult(() => apiGet(`/api/mcp/connector-action-requests/${request_id}`));
  }
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
    throw new Error(data?.error || `AIPermission API request failed with ${response.status}`);
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
      throw new Error(`AIPermission API request timed out after ${timeout}ms`);
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
