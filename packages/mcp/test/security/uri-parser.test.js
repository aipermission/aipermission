import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

const fromSDK = createRequire(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"));
const fromAjv = createRequire(fromSDK.resolve("ajv"));
const uri = fromAjv("fast-uri");

test("SDK URI resolver does not activate percent-encoded schemes", () => {
  for (const value of ["%6Aavascript:alert(1)", "%u006Aavascript:alert(1)"]) {
    assert.notEqual(uri.parse(value).scheme, "javascript");
    assert.doesNotMatch(uri.normalize(value), /^javascript:/i);
    assert.throws(() => uri.resolve("https://example.test/", value), /scheme is malformed/);
  }
  assert.equal(uri.equal("%68ttp://example.test/", "http://example.test/"), false);
});

test("SDK URI resolver preserves relative reference resolution", () => {
  const base = "https://example.test/a/b/";
  for (const reference of ["../x", "a/../b"]) {
    assert.equal(uri.resolve(base, uri.normalize(reference)), uri.resolve(base, reference));
  }
  assert.equal(uri.equal("../x", "x"), false);
});

test("SDK URI parser rejects ambiguous HTTP authorities", () => {
  for (const value of ["http://a@b@evil.example/", "http://exa\tmple.test/", "http://exa\nmple.test/", "http://exa\rmple.test/"]) {
    assert.ok(uri.parse(value).error, value);
  }
});

test("SDK suffix serialization cannot introduce a live URI scheme", () => {
  for (const scheme of ["javascript", "data", "vbscript"]) {
    const normalized = uri.serialize({ path: `${scheme}:alert(1)` }, { reference: "suffix" });
    assert.notEqual(uri.parse(normalized).scheme, scheme);
  }
});
