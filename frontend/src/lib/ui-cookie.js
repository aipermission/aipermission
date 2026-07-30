export function scopedUICookieName(base, location = browserLocation()) {
  const port = location?.port || defaultPort(location?.protocol);
  if (!port) return base;
  const scope = String(port).replace(/[^A-Za-z0-9_-]/g, "_");
  return scope ? `${base}_${scope}` : base;
}

function browserLocation() {
  return typeof window === "undefined" ? null : window.location;
}

function defaultPort(protocol) {
  if (protocol === "http:") return "80";
  if (protocol === "https:") return "443";
  return "";
}
