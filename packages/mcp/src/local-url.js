export const DEFAULT_API_URL = "http://localhost:3210";

const allowedLocalHosts = new Set(["localhost", "127.0.0.1", "::1"]);

export function normalizeLocalAPIURL(value = DEFAULT_API_URL) {
  const raw = String(value || DEFAULT_API_URL).trim();
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error("AIPERMISSION_API_URL must be a valid local HTTP URL.");
  }
  if (parsed.protocol !== "http:") {
    throw new Error("AIPERMISSION_API_URL must use http:// for the local gateway.");
  }
  if (parsed.username || parsed.password) {
    throw new Error("AIPERMISSION_API_URL must not contain URL credentials.");
  }
  let hostname = parsed.hostname.toLowerCase();
  if (hostname === "[::1]") {
    hostname = "::1";
  }
  if (!allowedLocalHosts.has(hostname)) {
    throw new Error("AIPERMISSION_API_URL must point to localhost, 127.0.0.1, or [::1].");
  }
  if ((parsed.pathname && parsed.pathname !== "/") || parsed.search || parsed.hash) {
    throw new Error("AIPERMISSION_API_URL must be the gateway origin only, for example http://localhost:3210.");
  }
  return parsed.toString().replace(/\/$/, "");
}
