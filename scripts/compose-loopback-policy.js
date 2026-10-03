const assert = require("node:assert/strict");

function assertComposeLoopback(config, frontendPort) {
  const fail = (condition) =>
    assert.ok(
      condition,
      "Compose loopback topology must keep frontend ingress and backend API isolated",
    );
  const { frontend, backend } = config.services || {};
  fail(frontend && backend);
  fail(Array.isArray(frontend.ports) && frontend.ports.length === 1);
  const port = frontend.ports[0];
  fail(
    port.host_ip === "127.0.0.1" &&
      port.target === 3210 &&
      String(port.published) === String(frontendPort) &&
      port.protocol === "tcp",
  );
  fail(
    backend.network_mode === "service:frontend" && backend.ports === undefined,
  );
  fail(
    backend.environment?.AIPERMISSION_BACKEND_HOST === "127.0.0.1" &&
      String(backend.environment?.AIPERMISSION_BACKEND_PORT) === "8080",
  );
}

module.exports = { assertComposeLoopback };
