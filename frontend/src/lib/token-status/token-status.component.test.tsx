import { expect, it } from "vitest";
import { isActiveToken, tokenStatus } from "../token-status";

it("uses the current clock for token status when no timestamp is supplied", () => {
  const now = Date.parse("2026-09-21T12:00:00Z");
  const originalNow = Date.now;
  Date.now = () => now;
  try {
    expect(tokenStatus({ expires_at: "2026-09-21T12:00:01Z" })).toBe("active");
    expect(isActiveToken({ expires_at: "2026-09-21T12:00:00Z" })).toBe(false);
  } finally {
    Date.now = originalNow;
  }
});
