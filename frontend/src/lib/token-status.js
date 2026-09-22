export function tokenStatus(token, now = Date.now()) {
  if (token?.revoked_at) return "revoked";
  if (!token?.expires_at) return "active";
  const expiresAt = Date.parse(token.expires_at);
  return Number.isFinite(expiresAt) && expiresAt > now ? "active" : "expired";
}

export function isActiveToken(token, now = Date.now()) {
  return tokenStatus(token, now) === "active";
}
