import { useEffect, useState } from "react";

const maximumTimerDelay = 2_147_483_647;

export function useTokenExpiryClock(tokens) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const currentTime = Date.now();
    const nextExpiry = (tokens || [])
      .filter((token) => !token?.revoked_at)
      .map((token) => Date.parse(token?.expires_at || ""))
      .filter((expiresAt) => Number.isFinite(expiresAt) && expiresAt > currentTime)
      .reduce((nearest, expiresAt) => Math.min(nearest, expiresAt), Number.POSITIVE_INFINITY);
    if (!Number.isFinite(nextExpiry)) return undefined;
    const timer = window.setTimeout(() => setNow(Date.now()), Math.min(maximumTimerDelay, Math.max(1, nextExpiry - currentTime + 1)));
    return () => window.clearTimeout(timer);
  }, [tokens, now]);

  return Math.max(now, Date.now());
}
