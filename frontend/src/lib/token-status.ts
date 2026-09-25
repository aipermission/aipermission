type TokenExpiry = { revoked_at?: string | null; expires_at?: string | null };

export function tokenStatus(token: TokenExpiry | null | undefined, now = Date.now()): "active" | "expired" | "revoked" {
  if (token?.revoked_at) return "revoked";
  if (!token?.expires_at) return "active";
  const expiresAt = Date.parse(token.expires_at);
  return Number.isFinite(expiresAt) && expiresAt > now ? "active" : "expired";
}

export function isActiveToken(token: TokenExpiry | null | undefined, now = Date.now()): boolean {
  return tokenStatus(token, now) === "active";
}
