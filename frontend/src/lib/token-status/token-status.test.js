import assert from "node:assert/strict";
import test from "node:test";
import { isActiveToken, tokenStatus } from "../token-status.js";

const now = Date.parse("2026-09-21T12:00:00Z");

test("classifies token activity consistently at expiry boundaries", () => {
  const cases = [
    [{}, "active"],
    [{ expires_at: "2026-09-21T12:00:01Z" }, "active"],
    [{ expires_at: "2026-09-21T12:00:00Z" }, "expired"],
    [{ expires_at: "2026-09-21T11:59:59Z" }, "expired"],
    [{ expires_at: "not-a-timestamp" }, "expired"],
    [{ revoked_at: "2026-09-20T12:00:00Z", expires_at: "2026-09-22T12:00:00Z" }, "revoked"],
  ];

  for (const [token, expected] of cases) {
    assert.equal(tokenStatus(token, now), expected);
    assert.equal(isActiveToken(token, now), expected === "active");
  }
});

test("uses the current clock when callers omit an explicit timestamp", () => {
  const originalNow = Date.now;
  Date.now = () => now;
  try {
    assert.equal(tokenStatus({ expires_at: "2026-09-21T12:00:01Z" }), "active");
    assert.equal(isActiveToken({ expires_at: "2026-09-21T12:00:00Z" }), false);
  } finally {
    Date.now = originalNow;
  }
});
