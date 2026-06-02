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
  version: "0.1.1",
});

server.tool(
  "list_servers",
  "List servers this aipermission token can access. Credentials are never returned.",
  {},
  async () => {
    return jsonToolResult(() => apiGet("/api/mcp/servers"));
  }
);

server.tool(
  "exec",
  "Execute a shell command on an allowed server through the local aipermission gateway. If status is approval_pending, follow assistant_hint and poll get_request. Long always_run commands return running; use read_console to continue watching.",
  {
    server_id: z.number().int().positive().describe("Server id from list_servers."),
    command: z.string().min(1).describe("Shell command to execute."),
    reason: z.string().optional().describe("Why this command is needed."),
  },
  async ({ server_id, command, reason }) => {
    return jsonToolResult(() => apiPost("/api/mcp/exec", {
      server_id,
      command,
      reason: reason || "",
    }));
  }
);

server.tool(
  "read_console",
  "Read the latest persistent console transcript for an allowed server. Use this after a long-running exec returns running.",
  {
    server_id: z.number().int().positive().describe("Server id from list_servers."),
    tail: z.number().int().positive().max(100000).optional().describe("Maximum transcript characters to return."),
  },
  async ({ server_id, tail }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams({ server_id: String(server_id) });
      if (tail) {
        params.set("tail", String(tail));
      }
      return apiGet(`/api/mcp/console?${params.toString()}`);
    });
  }
);

server.tool(
  "get_request",
  "Read an aipermission command request by id. Use this after exec returns approval_pending or running.",
  {
    request_id: z.number().int().positive().describe("Request id returned by exec."),
  },
  async ({ request_id }) => {
    return jsonToolResult(() => apiGet(`/api/mcp/requests/${request_id}`));
  }
);

server.tool(
  "list_requests",
  "List command requests for this token. Optionally filter by status such as pending_approval, running, completed, failed, declined, or error.",
  {
    status: z.string().optional().describe("Optional request status filter."),
  },
  async ({ status }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams();
      if (status) {
        params.set("status", status);
      }
      const suffix = params.toString() ? `?${params.toString()}` : "";
      return apiGet(`/api/mcp/requests${suffix}`);
    });
  }
);

server.tool(
  "send_message",
  "Send a short note to the aipermission Console messages panel for the human operator.",
  {
    message: z.string().min(1).describe("Message to show in the Console messages panel."),
    server_id: z.number().int().positive().optional().describe("Optional server id this message is about."),
    session_id: z.number().int().positive().optional().describe("Optional console session id this message is about."),
  },
  async ({ message, server_id, session_id }) => {
    return jsonToolResult(() => apiPost("/api/mcp/messages", {
      message,
      server_id: server_id || null,
      session_id: session_id || null,
    }));
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
    body: JSON.stringify(body),
  });
}

async function apiRequest(path, options) {
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
        "Content-Type": "application/json",
        Authorization: `Bearer ${apiToken}`,
        ...(options.headers || {}),
      },
    });
  } catch (error) {
    if (error?.name === "AbortError") {
      throw new Error(`aipermission API request timed out after ${timeout}ms`);
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
  const text = await response.text();
  const data = parseResponseBody(text);
  if (!response.ok) {
    throw new Error(data?.error || `aipermission API request failed with ${response.status}`);
  }
  return data;
}

function parseResponseBody(text) {
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    return { error: text.trim() || "Invalid non-JSON response from aipermission gateway." };
  }
}
