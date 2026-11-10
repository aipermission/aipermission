import { randomUUID } from "node:crypto";
import { z } from "zod";
import { idempotencyKeySchema } from "./idempotency-key.js";
import { projectGatewaySuccess, responseContracts } from "./response-contracts.js";
import { jsonActionToolResult } from "./results.js";
import { externalActionAnnotations } from "./tool-annotations.js";
import { openUploadSource } from "./upload-source.js";
import { createUploadRootResolver } from "./upload-roots.js";

export const defaultUploadTimeoutMs = 10 * 60_000;
export function connectorActionsPath(targetRef) {
  const query = new URLSearchParams({ target_ref: targetRef, upload: "stream-v1" });
  return `/api/mcp/connector-actions?${query}`;
}
const uploadInput = z
  .object({
    remote_path: z.string().min(1).describe("Exact destination file path, not a directory."),
    overwrite: z.boolean().default(false).describe("Only enable with explicit permission to replace the destination."),
  })
  .strict();

export function registerUploadTool(server, { apiGet, apiRequest, configuredRoots, timeoutMs }) {
  const resolveRoots = createUploadRootResolver(server.server, { configuredRoots });
  let active = 0;
  const upload = async (args, signal) => {
    if (active >= 2) throw new Error("Two local uploads are already in progress. Retry after one finishes.");
    active += 1;
    try {
      const deadline = AbortSignal.timeout(timeoutMs);
      return await uploadConnectorFile(args, {
        apiGet,
        apiRequest,
        resolveRoots,
        signal: signal ? AbortSignal.any([signal, deadline]) : deadline,
      });
    } finally {
      active -= 1;
    }
  };
  server.tool(
    "upload_connector_file",
    "Stream a client-local file from the current MCP workspace or explicitly configured upload directory into a connector action that advertises max_upload_bytes. Follow approval and check the transfer status before reporting success.",
    {
      target_ref: z.string().trim().min(1),
      action_name: z.string().trim().min(1),
      local_path: z
        .string()
        .min(1)
        .describe("Absolute file path inside the MCP workspace or configured upload roots on the client's machine."),
      input: uploadInput,
      reason: z.string().trim().min(1),
      idempotency_key: idempotencyKeySchema,
    },
    externalActionAnnotations,
    async (args, { signal }) =>
      jsonActionToolResult(
        () => upload(args, signal),
        (value) =>
          projectGatewaySuccess(responseContracts.connectorActionCall, value, {
            target_ref: args.target_ref,
            action_name: args.action_name,
          }),
        { idempotencyKey: args.idempotency_key },
      ),
  );
}

async function uploadConnectorFile(args, { apiGet, apiRequest, resolveRoots, signal }) {
  const scope = await resolveRoots(signal);
  const roots = scope.roots;
  signal = scope.signal;
  if (!roots.length) throw new Error("Local uploads are disabled. Configure AIPERMISSION_UPLOAD_ROOTS on the MCP client.");
  signal.throwIfAborted();
  const actions = projectGatewaySuccess(
    responseContracts.connectorActions,
    await apiGet(connectorActionsPath(args.target_ref), { signal }),
  );
  const limit = actions.items.find((item) => item.name === args.action_name)?.max_upload_bytes;
  if (!limit) throw new Error("This connector action does not support streamed uploads or is not permitted.");
  const source = await openUploadSource(args.local_path, roots, limit);
  try {
    const checksum = await source.checksum(signal);
    const metadata = {
      target_ref: args.target_ref,
      action_name: args.action_name,
      reason: args.reason,
      idempotency_key: args.idempotency_key,
      manifest: {
        file_name: source.name,
        remote_path: args.input.remote_path,
        size_bytes: source.size,
        checksum_sha256: checksum,
        overwrite: args.input.overwrite,
      },
    };
    const boundary = `aipermission-${randomUUID()}`;
    const body = uploadBody(boundary, metadata, source, signal);
    return await apiRequest(
      "/api/mcp/connector-actions/upload",
      {
        method: "POST",
        headers: { "Content-Type": `multipart/form-data; boundary=${boundary}` },
        redirect: "error",
        duplex: "half",
        body,
      },
      args.idempotency_key,
      { signal },
    );
  } finally {
    await source.close();
  }
}

async function* uploadBody(boundary, metadata, source, signal) {
  yield Buffer.from(
    `--${boundary}\r\nContent-Disposition: form-data; name="metadata"\r\nContent-Type: application/json\r\n\r\n${JSON.stringify(metadata)}\r\n`,
  );
  yield Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"\r\nContent-Type: application/octet-stream\r\n\r\n`);
  yield* source.chunks(signal);
  yield Buffer.from(`\r\n--${boundary}--\r\n`);
}
