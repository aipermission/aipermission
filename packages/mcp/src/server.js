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
  version: "0.1.2",
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

server.tool(
  "list_file_transfers",
  "List file transfer records visible to this token. Results never include local temp paths or archive paths.",
  {
    server_id: z.number().int().positive().optional().describe("Optional server id from list_servers."),
    direction: z.enum(["upload", "download"]).optional().describe("Optional transfer direction."),
    status: z.enum(["pending", "running", "paused", "completed", "failed", "canceled"]).optional().describe("Optional transfer status."),
    limit: z.number().int().positive().max(100).optional().describe("Maximum records to return."),
    offset: z.number().int().min(0).optional().describe("Pagination offset."),
  },
  async ({ server_id, direction, status, limit, offset }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams();
      if (server_id) params.set("server_id", String(server_id));
      if (direction) params.set("direction", direction);
      if (status) params.set("status", status);
      if (limit) params.set("limit", String(limit));
      if (offset) params.set("offset", String(offset));
      const suffix = params.toString() ? `?${params.toString()}` : "";
      return apiGet(`/api/mcp/file-transfers${suffix}`);
    });
  }
);

server.tool(
  "get_file_transfer",
  "Read one file transfer record visible to this token. Local temp paths and archive paths are never returned.",
  {
    transfer_id: z.number().int().positive().describe("Transfer id from list_file_transfers or get_file_transfer_batch."),
  },
  async ({ transfer_id }) => {
    return jsonToolResult(() => apiGet(`/api/mcp/file-transfers/${transfer_id}`));
  }
);

server.tool(
  "list_file_transfer_batches",
  "List file transfer queues visible to this token. Use get_file_transfer_batch for per-file progress.",
  {
    server_id: z.number().int().positive().optional().describe("Optional server id from list_servers."),
    direction: z.enum(["upload", "download"]).optional().describe("Optional transfer direction."),
    status: z.enum(["pending", "running", "paused", "completed", "failed", "canceled"]).optional().describe("Optional transfer status."),
    limit: z.number().int().positive().max(100).optional().describe("Maximum records to return."),
    offset: z.number().int().min(0).optional().describe("Pagination offset."),
  },
  async ({ server_id, direction, status, limit, offset }) => {
    return jsonToolResult(() => {
      const params = new URLSearchParams();
      if (server_id) params.set("server_id", String(server_id));
      if (direction) params.set("direction", direction);
      if (status) params.set("status", status);
      if (limit) params.set("limit", String(limit));
      if (offset) params.set("offset", String(offset));
      const suffix = params.toString() ? `?${params.toString()}` : "";
      return apiGet(`/api/mcp/file-transfer-batches${suffix}`);
    });
  }
);

server.tool(
  "get_file_transfer_batch",
  "Read one file transfer queue with per-file progress. Completed downloads are staged for the human operator to save from the UI.",
  {
    batch_id: z.number().int().positive().describe("Batch id from list_file_transfer_batches or start_file_download."),
  },
  async ({ batch_id }) => {
    return jsonToolResult(() => apiGet(`/api/mcp/file-transfer-batches/${batch_id}`));
  }
);

server.tool(
  "browse_remote_files",
  "Browse a remote server directory through AIPermission. Requires always_run permission. This lists remote metadata only and does not read local files.",
  {
    server_id: z.number().int().positive().describe("Server id from list_servers."),
    path: z.string().optional().describe("Absolute remote directory path. Defaults to /."),
  },
  async ({ server_id, path }) => {
    return jsonToolResult(() => apiPost("/api/mcp/file-transfers/browse", {
      server_id,
      path: path || "/",
    }));
  }
);

server.tool(
  "start_file_download",
  "Start a remote file download queue through AIPermission. Requires always_run permission. The AI cannot receive file contents; completed files are staged for the human operator to save from the UI.",
  {
    server_id: z.number().int().positive().describe("Server id from list_servers."),
    remote_paths: z.array(z.string().min(1)).min(1).max(100).describe("Absolute remote file paths to download sequentially."),
    archive_name: z.string().optional().describe("Optional archive filename for multi-file downloads."),
  },
  async ({ server_id, remote_paths, archive_name }) => {
    return jsonToolResult(() => apiPost("/api/mcp/file-transfers/download-batch", {
      server_id,
      remote_paths,
      archive_name: archive_name || "",
    }));
  }
);

server.tool(
  "pause_file_transfer_batch",
  "Pause an active file transfer queue started or visible through AIPermission. Requires always_run permission for that server.",
  {
    batch_id: z.number().int().positive().describe("Batch id from list_file_transfer_batches or start_file_download."),
  },
  async ({ batch_id }) => {
    return jsonToolResult(() => apiPost(`/api/mcp/file-transfer-batches/${batch_id}/pause`, {}));
  }
);

server.tool(
  "resume_file_transfer_batch",
  "Resume a paused file transfer queue. Requires always_run permission for that server.",
  {
    batch_id: z.number().int().positive().describe("Batch id from list_file_transfer_batches or start_file_download."),
  },
  async ({ batch_id }) => {
    return jsonToolResult(() => apiPost(`/api/mcp/file-transfer-batches/${batch_id}/resume`, {}));
  }
);

server.tool(
  "cancel_file_transfer_batch",
  "Cancel a pending, running, or paused file transfer queue. Requires always_run permission for that server.",
  {
    batch_id: z.number().int().positive().describe("Batch id from list_file_transfer_batches or start_file_download."),
  },
  async ({ batch_id }) => {
    return jsonToolResult(() => apiPost(`/api/mcp/file-transfer-batches/${batch_id}/cancel`, {}));
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
