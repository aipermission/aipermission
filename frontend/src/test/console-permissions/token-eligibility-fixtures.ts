export const tokenEligibilityNow = Date.parse("2026-08-01T12:00:00Z");
export const tokenEligibilityBoundary = "2026-08-01T12:00:01Z";
export const eligibilityTokens = [
  { id: 5, name: "permanent" },
  { id: 6, name: "future", expires_at: tokenEligibilityBoundary },
  { id: 7, name: "expired", expires_at: "2026-08-01T11:59:59Z" },
  { id: 8, name: "at-boundary", expires_at: "2026-08-01T12:00:00Z" },
  { id: 9, name: "invalid", expires_at: "not-a-timestamp" },
  { id: 10, name: "revoked", revoked_at: "2026-07-01T00:00:00Z", expires_at: tokenEligibilityBoundary },
];
