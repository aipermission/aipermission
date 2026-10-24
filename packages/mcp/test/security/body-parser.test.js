import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import test from "node:test";

function runFixture(source) {
  const fromSDK = createRequire(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"));
  const fromExpress = createRequire(fromSDK.resolve("express"));
  const result = spawnSync(process.execPath, ["--eval", source, fromExpress.resolve("body-parser")], {
    encoding: "utf8",
    timeout: 10000,
    maxBuffer: 64 * 1024,
    windowsHide: true,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
}

test("SDK HTTP parser survives trailing compressed bytes without losing request completion", () => {
  const source = `
    const assert = require("node:assert/strict");
    const http = require("node:http");
    const zlib = require("node:zlib");
    const parser = require(process.argv[1]).json();
    const trailing = Buffer.alloc(1024);
    const first = Buffer.concat([zlib.deflateSync(JSON.stringify({ ok: true })), Buffer.from([0])]);
    let client;
    const server = http.createServer((request, response) => {
      parser(request, response, (error) => {
        if (error) throw error;
        request.once("end", () => response.end(JSON.stringify(request.body)));
        client.end(trailing);
      });
    });
    server.listen(0, "127.0.0.1", () => {
      client = http.request({
        host: "127.0.0.1", port: server.address().port, method: "POST", agent: false,
        headers: { "Content-Type": "application/json", "Content-Encoding": "deflate", "Content-Length": first.length + trailing.length },
      }, (response) => {
        let text = "";
        response.setEncoding("utf8");
        response.on("data", (chunk) => { text += chunk; });
        response.on("end", () => {
          assert.equal(response.statusCode, 200);
          assert.deepEqual(JSON.parse(text), { ok: true });
          server.close();
        });
      });
      client.on("error", (error) => { throw error; });
      client.setTimeout(3000, () => client.destroy(new Error("fixture request timed out")));
      client.write(first);
    });
  `;
  runFixture(source);
});

test("SDK HTTP parser rejects unsupported charsets without leaving an unhandled decoder", () => {
  runFixture(`
    const assert = require("node:assert/strict");
    const http = require("node:http");
    const zlib = require("node:zlib");
    const parser = require(process.argv[1]).json({ verify() {} });
    const body = Buffer.concat([zlib.gzipSync("{}"), Buffer.from([0]), Buffer.alloc(100 * 1024, 0x58)]);
    const server = http.createServer((request, response) => {
      parser(request, response, (error) => {
        assert.equal(error?.status, 415);
        request.resume();
        response.statusCode = 415;
        response.end("unsupported charset");
      });
    });
    server.listen(0, "127.0.0.1", () => {
      const client = http.request({
        host: "127.0.0.1", port: server.address().port, method: "POST", agent: false,
        headers: { "Content-Type": "application/json; charset=utf-999", "Content-Encoding": "gzip", "Content-Length": body.length },
      }, (response) => {
        assert.equal(response.statusCode, 415);
        response.resume();
        response.on("end", () => setTimeout(() => server.close(), 150));
      });
      client.on("error", (error) => { throw error; });
      client.setTimeout(3000, () => client.destroy(new Error("fixture request timed out")));
      client.end(body);
    });
  `);
});

test("SDK default HTTP parser tears down a completed gzip decoder before trailing bytes arrive", () => {
  runFixture(`
    const assert = require("node:assert/strict");
    const http = require("node:http");
    const zlib = require("node:zlib");
    const parser = require(process.argv[1]).json();
    const body = Buffer.concat([zlib.gzipSync('{"ok":true}'), Buffer.from([0]), Buffer.alloc(100 * 1024, 0x58)]);
    const server = http.createServer((request, response) => {
      parser(request, response, (error) => {
        if (error) throw error;
        response.end(JSON.stringify(request.body));
      });
    });
    server.listen(0, "127.0.0.1", () => {
      const client = http.request({
        host: "127.0.0.1", port: server.address().port, method: "POST", agent: false,
        headers: { "Content-Type": "application/json", "Content-Encoding": "gzip", "Content-Length": body.length },
      }, (response) => {
        let text = "";
        response.setEncoding("utf8");
        response.on("data", (chunk) => { text += chunk; });
        response.on("end", () => setTimeout(() => {
          assert.equal(response.statusCode, 200);
          assert.deepEqual(JSON.parse(text), { ok: true });
          server.close();
        }, 200));
      });
      client.on("error", (error) => { throw error; });
      client.setTimeout(3000, () => client.destroy(new Error("fixture request timed out")));
      client.end(body);
    });
  `);
});
