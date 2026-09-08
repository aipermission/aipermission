import assert from "node:assert/strict";
import test from "node:test";
import { apiSource, nginxSource } from "./app-smoke-fixtures.js";

test("MCP setup defaults to the local Docker frontend origin", () => {
  assert.match(apiSource, /mcpApiUrl = normalizeApiUrl\(viteEnv\.VITE_MCP_API_URL \|\| browserOrigin\(\)\)/);
  assert.doesNotMatch(apiSource, /mcpApiUrl[\s\S]*"http:\/\/localhost:8080"/);
});

test("nginx CSP keeps browser connections local plus manual update checks", () => {
  assert.match(nginxSource, /connect-src 'self'/);
  assert.match(nginxSource, /https:\/\/api\.github\.com/);
  assert.match(nginxSource, /object-src 'none'/);
  assert.match(nginxSource, /form-action 'self'/);
  assert.match(nginxSource, /frame-src 'none'/);
  assert.match(nginxSource, /worker-src 'self' blob:/);
  assert.match(nginxSource, /style-src 'self'; style-src-attr 'unsafe-inline'; style-src-elem 'self' 'unsafe-inline'/);
  assert.match(nginxSource, /Permissions-Policy/);
  assert.match(nginxSource, /Cross-Origin-Opener-Policy "same-origin"/);
  assert.match(nginxSource, /Cross-Origin-Resource-Policy "same-origin"/);
  assert.match(nginxSource, /proxy_hide_header X-Content-Type-Options/);
  assert.doesNotMatch(nginxSource, /ws:\/\/localhost:3210/);
  assert.doesNotMatch(nginxSource, /ws:\/\/localhost:\*/);
});

test("nginx accepts each supported loopback Host spelling", () => {
  assert.match(nginxSource, /localhost 0/);
  assert.match(nginxSource, /127\.0\.0\.1 0/);
  assert.match(nginxSource, /"::1" 0/);
  assert.match(nginxSource, /"\[::1\]" 0/);
});

test("nginx keeps route-specific upload limits and JSON error responses aligned", () => {
  assert.match(nginxSource, /server \{[\s\S]*client_max_body_size 1m/);
  assert.match(nginxSource, /location = \/api\/connector-actions\/local-run[\s\S]*client_max_body_size 32m/);
  assert.match(nginxSource, /location = \/api\/mcp\/connector-actions\/call[\s\S]*client_max_body_size 32m/);
  assert.match(nginxSource, /location = \/api\/backup\/import[\s\S]*client_max_body_size 256m/);
  assert.match(nginxSource, /location ~ \^\/api\/connector-targets\/[\s\S]*client_max_body_size 256m/);
  assert.match(nginxSource, /location = \/api\/file-transfers\/upload[\s\S]*client_max_body_size 528m/);
  assert.match(nginxSource, /location = \/api\/file-transfers\/upload-batch[\s\S]*client_max_body_size 1040m/);
  assert.match(nginxSource, /error_page 413 = @json_payload_too_large/);
  assert.match(nginxSource, /request_body_too_large/);
  assert.match(nginxSource, /Uploaded database is too large/);
  assert.match(nginxSource, /Uploaded restore file is too large/);
  assert.match(nginxSource, /Maximum file size is 512 MiB/);
  assert.match(nginxSource, /Maximum batch size is 1 GiB/);
  assert.doesNotMatch(nginxSource, /proxy_intercept_errors\s+on/);
  assert.doesNotMatch(nginxSource, /error_page 502 503 504/);
});
