import { objectRecord } from "../../../lib/api-types";
import type { CleanupIdentity } from "./cleanup-types";

export function cleanupIdentity(value: unknown): CleanupIdentity {
  const identity = objectRecord(value);
  if (
    !identity ||
    Object.keys(identity).length !== 9 ||
    !positiveID(identity.target_id) ||
    !cleanupText(identity.target_revision) ||
    !cleanupDigest(identity.config_digest) ||
    !cleanupText(identity.host) ||
    !cleanupPort(identity.port) ||
    !cleanupText(identity.username) ||
    !cleanupDigest(identity.key_digest) ||
    !cleanupPins(identity.host_fingerprints) ||
    !Array.isArray(identity.profiles) ||
    identity.profiles.length === 0
  )
    throw new Error("Invalid SSH cleanup identity.");
  const profiles = identity.profiles.map((value) => {
    const profile = objectRecord(value);
    if (
      !profile ||
      Object.keys(profile).length !== 6 ||
      !positiveID(profile.id) ||
      !positiveID(profile.key_id) ||
      !cleanupText(profile.revision) ||
      !cleanupText(profile.secret_revision) ||
      !cleanupText(profile.key_revision) ||
      !cleanupDigest(profile.public_digest)
    )
      throw new Error("Invalid SSH cleanup profile identity.");
    return {
      id: profile.id,
      revision: profile.revision,
      secret_revision: profile.secret_revision,
      public_digest: profile.public_digest,
      key_id: profile.key_id,
      key_revision: profile.key_revision,
    };
  });
  if (!profiles.every((profile, index) => index === 0 || profiles[index - 1].id < profile.id))
    throw new Error("Invalid SSH cleanup profile order.");
  // Fixed wire field order also makes acknowledgement comparison independent of JSON object key order.
  return {
    target_id: identity.target_id,
    target_revision: identity.target_revision,
    config_digest: identity.config_digest,
    host: identity.host,
    port: identity.port,
    username: identity.username,
    key_digest: identity.key_digest,
    host_fingerprints: [...identity.host_fingerprints],
    profiles,
  };
}

export function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
export function cleanupDigest(value: unknown): value is string {
  return typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
}
export function cleanupText(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}
export function cleanupPort(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value > 0 && value <= 65535;
}
export function cleanupFingerprint(value: unknown): value is string {
  if (typeof value !== "string" || !/^SHA256:[A-Za-z0-9+/]{43}$/.test(value)) return false;
  const encoded = value.slice(7);
  return btoa(atob(encoded)).replace(/=+$/, "") === encoded;
}
export function cleanupPins(value: unknown): value is string[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every((pin, index) => cleanupFingerprint(pin) && (index === 0 || value[index - 1] < pin))
  );
}

export function cleanupReason(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const trimmed = value.trim();
  return trimmed.length > 0 && new TextEncoder().encode(trimmed).byteLength <= 2000;
}
