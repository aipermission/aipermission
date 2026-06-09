#!/usr/bin/env node

if (process.argv[2] === "init") {
  const { runInit } = await import("./init.js");
  await runInit(process.argv.slice(3));
  process.exit(0);
}

import fs from "node:fs";
import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { normalizeLocalAPIURL } from "./local-url.js";
import { jsonToolResult } from "./results.js";

const apiUrl = normalizeLocalAPIURL(process.env.AIPERMISSION_API_URL);
const apiToken = process.env.AIPERMISSION_API_TOKEN || "";
const apiTimeoutMs = Number.parseInt(process.env.AIPERMISSION_HTTP_TIMEOUT_MS || "60000", 10);
const apiTransferTimeoutMs = Number.parseInt(process.env.AIPERMISSION_TRANSFER_TIMEOUT_MS || "7200000", 10);

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
  "Execute a shell command on one allowed server, or the same shell command across multiple allowed servers, through the local aipermission gateway. If status is approval_pending, follow assistant_hint and poll get_request. Long always_run commands return running; use read_console to continue watching.",
  {
    server_id: z.number().int().positive().optional().describe("Single server id from list_servers. Use either server_id or server_ids, not both."),
    server_ids: z.array(z.number().int().positive()).min(1).max(25).optional().describe("Multiple server ids from list_servers for bulk execution. Up to 25 targets. Use either server_id or server_ids, not both."),
    command: z.string().min(1).describe("Shell command to execute."),
    reason: z.string().optional().describe("Why this command is needed. Required when using server_ids."),
  },
  async ({ server_id, server_ids, command, reason }) => {
    return jsonToolResult(() => {
      if (server_id && server_ids?.length) {
        throw new Error("Use either server_id or server_ids, not both.");
      }
      if (!server_id && !server_ids?.length) {
        throw new Error("server_id or server_ids is required.");
      }
      if (server_ids?.length) {
        if (!String(reason || "").trim()) {
          throw new Error("reason is required when using server_ids.");
        }
        return apiPost("/api/mcp/bulk-exec", {
          server_ids,
          command,
          reason,
        });
      }
      return apiPost("/api/mcp/exec", {
        server_id,
        command,
        reason: reason || "",
      });
    });
  }
);

server.tool(
  "read_console",
  "Read the latest persistent console transcript for one allowed server, or for multiple allowed servers after a multi-server exec. Use this after a long-running exec returns running.",
  {
    server_id: z.number().int().positive().optional().describe("Single server id from list_servers. Use either server_id or server_ids, not both."),
    server_ids: z.array(z.number().int().positive()).min(1).max(25).optional().describe("Multiple server ids from list_servers. Up to 25 targets. Use either server_id or server_ids, not both."),
    tail: z.number().int().positive().max(100000).optional().describe("Maximum transcript characters to return."),
  },
  async ({ server_id, server_ids, tail }) => {
    return jsonToolResult(async () => {
      if (server_id && server_ids?.length) {
        throw new Error("Use either server_id or server_ids, not both.");
      }
      if (!server_id && !server_ids?.length) {
        throw new Error("server_id or server_ids is required.");
      }
      const readOne = (id) => {
        const params = new URLSearchParams({ server_id: String(id) });
        if (tail) {
          params.set("tail", String(tail));
        }
        return apiGet(`/api/mcp/console?${params.toString()}`);
      };
      if (server_ids?.length) {
        const unique = new Set(server_ids);
        if (unique.size !== server_ids.length) {
          throw new Error("server_ids must not contain duplicates.");
        }
        const items = await Promise.all(server_ids.map(async (id) => {
          try {
            return await readOne(id);
          } catch (error) {
            return {
              status: "error",
              server_id: id,
              error: error?.message || "failed to read console",
            };
          }
        }));
        return {
          status: "ok",
          items,
          assistant_hint: "Inspect each item independently. A listed server may have no active console, blocked read permission, or an SSH/session error.",
        };
      }
      return readOne(server_id);
    });
  }
);

server.tool(
  "restart_console_session",
  "Restart the persistent console session for a server when it appears stuck. This closes the current gateway-owned console session and the next exec will open a fresh SSH session.",
  {
    server_id: z.number().int().positive().describe("Server id from list_servers."),
  },
  async ({ server_id }) => {
    return jsonToolResult(() => apiPost("/api/mcp/console/restart", {
      server_id,
    }));
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
  "List file transfer records visible to this token. Results never include local temp paths, archive paths, local upload contents, or file contents.",
  {
    server_id: z.number().int().positive().optional().describe("Optional server id from list_servers."),
    direction: z.enum(["upload", "download"]).optional().describe("Optional transfer direction."),
    status: z.enum(["pending_approval", "pending", "running", "paused", "completed", "failed", "canceled"]).optional().describe("Optional transfer status."),
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
  "Read one file transfer record visible to this token. Local temp paths, archive paths, local upload contents, and file contents are never returned.",
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
    status: z.enum(["pending_approval", "pending", "running", "paused", "completed", "failed", "canceled"]).optional().describe("Optional transfer status."),
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
  "Read one file transfer queue with per-file progress. Use save_file_download to write a completed MCP-started download to an explicit local path.",
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
  "Start a remote file download queue through AIPermission. always_run starts immediately; approval_required creates a local approval queue. Use get_file_transfer_batch for progress, then save_file_download to write a completed download to the local machine.",
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
  "save_file_download",
  "Save a completed MCP-started download batch to the local filesystem. File contents are written by the local MCP process and are not returned to the AI response.",
  {
    batch_id: z.number().int().positive().describe("Completed download batch id from start_file_download or list_file_transfer_batches."),
    local_path: z.string().min(1).describe("Local file path or existing directory where the completed download should be saved."),
    overwrite: z.boolean().optional().describe("Whether to overwrite an existing local file. Defaults to false."),
  },
  async ({ batch_id, local_path, overwrite }) => {
    return jsonToolResult(async () => {
      const batch = await apiGet(`/api/mcp/file-transfer-batches/${batch_id}`);
      if (batch?.direction !== "download") {
        throw new Error("batch is not a download");
      }
      if (batch?.status !== "completed") {
        throw new Error(`download batch is not completed; current status is ${batch?.status || "unknown"}`);
      }
      const filename = suggestedDownloadFilename(batch);
      const destination = await resolveLocalDestination(local_path, filename, Boolean(overwrite));
      const saved = await apiDownloadToFile(`/api/mcp/file-transfer-batches/${batch_id}/download`, destination, Boolean(overwrite));
      return {
        status: "saved",
        batch_id,
        local_path: saved.path,
        file_name: path.basename(saved.path),
        bytes_written: saved.bytes,
        assistant_hint: "The file was saved by the local MCP process. Do not print file contents unless the user explicitly asks you to inspect the saved file.",
      };
    });
  }
);

server.tool(
  "upload_files",
  "Upload local files to a remote server through AIPermission. always_run starts immediately; approval_required stages the files locally and waits for local approval before writing to the remote server. File contents are read by the local MCP process and are not returned to the AI response.",
  {
    server_id: z.number().int().positive().describe("Server id from list_servers."),
    local_paths: z.array(z.string().min(1)).min(1).max(100).describe("Local file paths to upload sequentially."),
    remote_dir: z.string().min(1).describe("Absolute remote directory where files should be uploaded."),
    overwrite: z.boolean().optional().describe("Whether to overwrite existing remote files. Defaults to false."),
  },
  async ({ server_id, local_paths, remote_dir, overwrite }) => {
    return jsonToolResult(async () => {
      const files = await resolveUploadFiles(local_paths);
      const batch = await apiPostMultipart("/api/mcp/file-transfers/upload-batch", {
        server_id: String(server_id),
        remote_dir,
        overwrite: overwrite ? "true" : "false",
      }, files);
      return batch;
    });
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
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

async function apiPostMultipart(path, fields, files) {
  const boundary = `aipermission-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return apiRequest(path, {
    method: "POST",
    headers: {
      "Content-Type": `multipart/form-data; boundary=${boundary}`,
    },
    body: Readable.from(multipartBody(boundary, fields, files)),
    duplex: "half",
    timeoutMs: apiTransferTimeoutMs,
  });
}

async function apiDownloadToFile(pathValue, destination, overwrite) {
  const response = await apiFetch(pathValue, {
    method: "GET",
    timeoutMs: apiTransferTimeoutMs,
  });
  if (!response.ok) {
    const text = await response.text();
    const data = parseResponseBody(text);
    throw new Error(data?.error || `aipermission API request failed with ${response.status}`);
  }
  let bytes = 0;
  const output = fs.createWriteStream(destination, {
    flags: overwrite ? "w" : "wx",
    mode: 0o600,
  });
  output.on("bytesWritten", (value) => {
    bytes = value;
  });
  await pipeline(Readable.fromWeb(response.body), output);
  const stat = await fsp.stat(destination);
  return { path: destination, bytes: stat.size || bytes };
}

async function apiRequest(path, options) {
  const response = await apiFetch(path, options);
  const text = await response.text();
  const data = parseResponseBody(text);
  if (!response.ok) {
    throw new Error(data?.error || `aipermission API request failed with ${response.status}`);
  }
  return data;
}

async function apiFetch(path, options) {
  if (!apiToken) {
    throw new Error("AIPERMISSION_API_TOKEN is required.");
  }
  const timeoutValue = options.timeoutMs || apiTimeoutMs;
  const timeout = Number.isFinite(timeoutValue) && timeoutValue > 0 ? timeoutValue : 60000;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);
  const { timeoutMs: _timeoutMs, ...requestOptions } = options;
  let response;
  try {
    response = await fetch(`${apiUrl}${path}`, {
      ...requestOptions,
      signal: controller.signal,
      headers: {
        Authorization: `Bearer ${apiToken}`,
        ...(requestOptions.headers || {}),
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
  return response;
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

async function resolveUploadFiles(localPaths) {
  const files = [];
  const seen = new Set();
  for (const rawPath of localPaths) {
    const resolved = expandHome(rawPath);
    if (seen.has(resolved)) {
      throw new Error(`duplicate local path: ${resolved}`);
    }
    seen.add(resolved);
    const stat = await fsp.stat(resolved).catch((error) => {
      throw new Error(`cannot read local file ${resolved}: ${error.message}`);
    });
    if (!stat.isFile()) {
      throw new Error(`local path is not a regular file: ${resolved}`);
    }
    files.push({
      field: "files",
      path: resolved,
      name: path.basename(resolved),
      size: stat.size,
    });
  }
  return files;
}

async function resolveLocalDestination(localPath, suggestedName, overwrite) {
  const resolved = expandHome(localPath);
  const existing = await fsp.stat(resolved).catch((error) => {
    if (error?.code === "ENOENT") return null;
    throw error;
  });
  if (existing?.isDirectory()) {
    return resolveLocalDestination(path.join(resolved, suggestedName), suggestedName, overwrite);
  }
  if (existing && !overwrite) {
    throw new Error(`local file already exists: ${resolved}`);
  }
  const parent = path.dirname(resolved);
  const parentStat = await fsp.stat(parent).catch((error) => {
    throw new Error(`local directory does not exist: ${parent}: ${error.message}`);
  });
  if (!parentStat.isDirectory()) {
    throw new Error(`local parent path is not a directory: ${parent}`);
  }
  return resolved;
}

function suggestedDownloadFilename(batch) {
  if (Array.isArray(batch?.items) && batch.items.length === 1 && batch.items[0]?.file_name) {
    return safeLocalFilename(batch.items[0].file_name);
  }
  if (batch?.archive_name) {
    return safeLocalFilename(batch.archive_name);
  }
  return `aipermission-download-${batch?.id || Date.now()}.zip`;
}

function safeLocalFilename(value) {
  const base = path.basename(String(value || "").replaceAll("\0", ""));
  return base || "aipermission-download";
}

function expandHome(value) {
  const text = String(value || "").trim();
  if (text === "~") return os.homedir();
  if (text.startsWith("~/")) return path.join(os.homedir(), text.slice(2));
  return path.resolve(text);
}

async function* multipartBody(boundary, fields, files) {
  for (const [name, value] of Object.entries(fields)) {
    yield Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="${escapeMultipartName(name)}"\r\n\r\n${String(value)}\r\n`);
  }
  for (const file of files) {
    yield Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="${escapeMultipartName(file.field)}"; filename="${escapeMultipartName(file.name)}"\r\nContent-Type: application/octet-stream\r\n\r\n`);
    yield* fs.createReadStream(file.path);
    yield Buffer.from("\r\n");
  }
  yield Buffer.from(`--${boundary}--\r\n`);
}

function escapeMultipartName(value) {
  return String(value).replaceAll("\\", "\\\\").replaceAll("\"", "\\\"");
}
