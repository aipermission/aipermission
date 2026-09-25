type CookieLocation = Pick<Location, "port" | "protocol">;

export function scopedUICookieName(base: string, location: CookieLocation | null = browserLocation()): string {
  const port = location?.port || defaultPort(location?.protocol);
  if (!port) return base;
  const scope = String(port).replace(/[^A-Za-z0-9_-]/g, "_");
  return scope ? `${base}_${scope}` : base;
}

function browserLocation(): CookieLocation | null {
  return typeof window === "undefined" ? null : window.location;
}

function defaultPort(protocol: string | undefined): string {
  if (protocol === "http:") return "80";
  if (protocol === "https:") return "443";
  return "";
}
