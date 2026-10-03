import { beforeEach, expect, it, vi } from "vitest";
import { invalidateUISession, subscribeUISessionInvalidation } from "./ui-session-events.ts";

class Channel {
  static instances: Channel[] = [];
  onmessage?: (_event: { data: unknown }) => void;
  closed = false;
  postMessage = vi.fn((data: unknown) => {
    for (const channel of Channel.instances) {
      if (channel !== this && !channel.closed) channel.onmessage?.({ data });
    }
  });
  constructor(public _name: string) {
    Channel.instances.push(this);
  }
  close() {
    this.closed = true;
  }
}

beforeEach(() => {
  Channel.instances = [];
  vi.stubGlobal("BroadcastChannel", Channel);
});

it("sends only invalidation and releases all channel/listener ownership", () => {
  const receive = vi.fn();
  const dispose = subscribeUISessionInvalidation(receive);
  invalidateUISession();
  expect(receive).toHaveBeenCalledTimes(1);
  expect(Channel.instances[1].postMessage).toHaveBeenCalledExactlyOnceWith({ kind: "invalidate", sender: expect.any(String) });
  expect(Channel.instances[1].closed).toBe(true);
  Channel.instances[0].onmessage?.({ data: { kind: "invalidate", sender: "other-tab" } });
  expect(receive).toHaveBeenCalledTimes(2);
  Channel.instances[0].onmessage?.({ data: { state: "unlocked", token: "untrusted-notification" } });
  expect(receive).toHaveBeenCalledTimes(2);
  dispose();
  expect(Channel.instances[0].closed).toBe(true);
  window.dispatchEvent(new Event("aipermission:ui-session-required"));
  expect(receive).toHaveBeenCalledTimes(2);
});

it("uses value-free storage invalidation if channels are unavailable and tolerates blocked storage", () => {
  vi.stubGlobal("BroadcastChannel", undefined);
  const receive = vi.fn();
  const dispose = subscribeUISessionInvalidation(receive);
  invalidateUISession();
  const key = "aipermission:ui-session-invalidation";
  expect(window.localStorage.getItem(key)).toMatch(/^invalidate:/);
  window.dispatchEvent(new StorageEvent("storage", { key, newValue: "invalidate:one" }));
  window.dispatchEvent(new StorageEvent("storage", { key, newValue: "unlocked" }));
  window.dispatchEvent(new StorageEvent("storage", { key: "unrelated", newValue: "invalidate:one" }));
  expect(receive).toHaveBeenCalledTimes(2);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("storage denied");
  });
  expect(() => invalidateUISession()).not.toThrow();
  expect(receive).toHaveBeenCalledTimes(3);
  dispose();
  window.dispatchEvent(new StorageEvent("storage", { key, newValue: "invalidate:late" }));
  expect(receive).toHaveBeenCalledTimes(3);
});
