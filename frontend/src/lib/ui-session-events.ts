import { writeLocalPreference } from "./browser-storage.ts";

const localEvent = "aipermission:ui-session-required";
const channelName = "aipermission:ui-session-invalidation";
const invalidationKey = "aipermission:ui-session-invalidation";
const invalidation = "invalidate";
const sender = globalThis.crypto?.randomUUID?.() || `${Date.now()}:${Math.random()}`;

// Same-origin notifications carry no database identity, authorization or
// secret. They only revoke cached UI state; the gateway decides what may reopen.
export function invalidateUISession() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(localEvent));
  try {
    const channel = new window.BroadcastChannel(channelName);
    channel.postMessage({ kind: invalidation, sender });
    channel.close();
  } catch {
    // This nonce only ensures a storage event, never grants authorization.
    writeLocalPreference(invalidationKey, `${invalidation}:${Date.now()}:${Math.random()}`);
  }
}

export function subscribeUISessionInvalidation(onInvalidate: () => void) {
  window.addEventListener(localEvent, onInvalidate);
  const onStorage = (event: StorageEvent) => {
    if (event.key === invalidationKey && event.newValue?.startsWith(`${invalidation}:`)) onInvalidate();
  };
  window.addEventListener("storage", onStorage);
  let channel: BroadcastChannel | undefined;
  try {
    channel = new window.BroadcastChannel(channelName);
    channel.onmessage = (event) => {
      const data: unknown = event.data;
      if (
        data &&
        typeof data === "object" &&
        "kind" in data &&
        data.kind === invalidation &&
        "sender" in data &&
        typeof data.sender === "string" &&
        data.sender !== sender
      )
        onInvalidate();
    };
  } catch {
    // Storage events and focus/status checks remain available.
  }
  return () => {
    window.removeEventListener(localEvent, onInvalidate);
    window.removeEventListener("storage", onStorage);
    channel?.close();
  };
}
