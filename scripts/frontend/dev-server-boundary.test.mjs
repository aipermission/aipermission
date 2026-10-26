import assert from "node:assert/strict";
import fs from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import {
  createServer,
  resolveConfig,
} from "../../frontend/node_modules/vite/dist/node/index.js";

const root = fileURLToPath(new URL("../../frontend", import.meta.url));
const base = {
  root,
  logLevel: "silent",
  optimizeDeps: { noDiscovery: true, entries: [] },
};

function listenOnLoopback(listener) {
  return new Promise((resolve, reject) => {
    listener.once("error", reject);
    listener.listen(0, "127.0.0.1", resolve);
  });
}

function closeListener(listener) {
  return new Promise((resolve, reject) => {
    listener.close((error) => (error ? reject(error) : resolve()));
  });
}

test("actual Vite config separates source dev from the persistent gateway", async () => {
  const config = await resolveConfig(base, "serve");
  assert.equal(config.server.host, "127.0.0.1");
  assert.equal(config.server.port, 3213);
  assert.equal(config.server.strictPort, true);
});

test("Vite refuses an occupied requested port instead of silently switching", async () => {
  const occupied = net.createServer();
  let server;
  try {
    await listenOnLoopback(occupied);
    const port = occupied.address().port;
    server = await createServer({ ...base, server: { port } });
    await assert.rejects(() => server.listen(), /already in use/);
  } finally {
    await server?.close();
    if (occupied.listening) await closeListener(occupied);
  }
});

test("Vite binds the explicitly requested ephemeral loopback port", async () => {
  const reservation = net.createServer();
  let server;
  try {
    await listenOnLoopback(reservation);
    const port = reservation.address().port;
    await closeListener(reservation);
    server = await createServer({ ...base, server: { port } });
    await server.listen();
    const address = server.httpServer.address();
    assert.equal(address.address, "127.0.0.1");
    assert.equal(address.port, port);
    assert.ok(![3210, 3212, 3213].includes(address.port));
    assert.equal(path.resolve(server.config.root), path.resolve(root));
  } finally {
    await server?.close();
    if (reservation.listening) await closeListener(reservation);
  }
});

test("Vite applies filesystem denial before and after stripping transform queries", { timeout: 30_000 }, async () => {
  const fixture = await fs.mkdtemp(path.join(os.tmpdir(), "vite-fs-deny-"));
  let server;
  try {
    await fs.writeFile(path.join(fixture, "visible.txt"), "visible fixture payload");
    await fs.writeFile(path.join(fixture, "blocked.txt"), "denied fixture payload");
    server = await createServer({
      configFile: false,
      root: fixture,
      logLevel: "silent",
      optimizeDeps: { noDiscovery: true, entries: [] },
      server: {
        host: "127.0.0.1",
        port: 0,
        strictPort: true,
        fs: { strict: true, allow: [fixture], deny: ["**/blocked.txt"] },
        watch: { ignored: ["**"] },
      },
    });
    await server.listen();
    const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
    const visible = await fetch(`${origin}/visible.txt?raw`, { signal: AbortSignal.timeout(10_000) });
    assert.equal(visible.status, 200);
    assert.ok((await visible.text()).includes("visible fixture payload"));
    for (const query of ["", "?raw", "?url", "?inline", "?import&raw", "?.svg?import"]) {
      const response = await fetch(`${origin}/blocked.txt${query}`, { signal: AbortSignal.timeout(10_000) });
      assert.equal(response.status, 403, query || "no query");
      assert.ok(!(await response.text()).includes("denied fixture payload"));
    }
  } finally {
    await server?.close();
    await fs.rm(fixture, { recursive: true, force: true });
  }
});
