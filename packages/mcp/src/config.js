import { connectorActionDefaultClientTimeoutMs } from "./generated-connector-contract.js";
import { Agent } from "undici";

const defaultHTTPTimeoutMs = connectorActionDefaultClientTimeoutMs;
const maxHTTPTimeoutMs = 10 * 60_000;

export function createHTTPDispatcher(timeout) {
  const budget = parseHTTPTimeout(timeout);
  return new Agent({
    headersTimeout: budget,
    bodyTimeout: budget,
    connect: { timeout: Math.min(budget, 10_000) },
  });
}

export function parseHTTPTimeout(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return defaultHTTPTimeoutMs;
  if (!/^[1-9]\d*$/.test(raw)) {
    throw new Error("AIPERMISSION_HTTP_TIMEOUT_MS must be a positive integer in milliseconds.");
  }
  const timeout = Number(raw);
  if (!Number.isSafeInteger(timeout) || timeout > maxHTTPTimeoutMs) {
    throw new Error(`AIPERMISSION_HTTP_TIMEOUT_MS must not exceed ${maxHTTPTimeoutMs} milliseconds.`);
  }
  return timeout;
}
