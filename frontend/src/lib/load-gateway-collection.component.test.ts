import { beforeEach, expect, it, vi } from "vitest";
import type { SetStateAction } from "react";
import { apiGet } from "./api";
import { createRequestGuard } from "./request-guard";
import { loadGatewayCollection, type GatewayCollection } from "./load-gateway-collection";

vi.mock("./api", () => ({ apiGet: vi.fn() }));

function collection() {
  const guard = createRequestGuard("collection-test");
  let state: GatewayCollection<string> = { state: "ready", data: ["previous"], error: null };
  const decode = vi.fn((response: unknown) => {
    if (!Array.isArray(response) || !response.every((item): item is string => typeof item === "string")) throw new Error("Invalid items");
    return response;
  });
  const setState = (update: SetStateAction<GatewayCollection<string>>) => {
    state = typeof update === "function" ? update(state) : update;
  };
  return {
    guard,
    decode,
    get state() {
      return state;
    },
    load: () => loadGatewayCollection({ path: "/api/items", channel: "items", guard, decode, setState }),
  };
}

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
});

it("decodes responses and keeps existing data during loading", async () => {
  let complete!: (_response: unknown) => void;
  vi.mocked(apiGet).mockReturnValue(
    new Promise((resolve) => {
      complete = resolve;
    }),
  );
  const owner = collection();
  const loading = owner.load();
  expect(owner.state).toEqual({ state: "loading", data: ["previous"], error: null });
  complete(["new"]);
  expect(await loading).toEqual(["new"]);
  expect(owner.state).toEqual({ state: "ready", data: ["new"], error: null });
});

it("reports malformed responses and transport errors without retaining stale items", async () => {
  const owner = collection();
  vi.mocked(apiGet).mockResolvedValue([42]);
  expect(await owner.load()).toEqual([]);
  expect(owner.state).toEqual({ state: "error", data: [], error: "Invalid items" });
  vi.mocked(apiGet).mockRejectedValue(new Error("Unavailable"));
  await owner.load();
  expect(owner.state.error).toBe("Unavailable");
});

it("does not decode or publish a superseded response", async () => {
  let complete!: (_response: unknown) => void;
  vi.mocked(apiGet)
    .mockReturnValueOnce(
      new Promise((resolve) => {
        complete = resolve;
      }),
    )
    .mockResolvedValueOnce(["latest"]);
  const owner = collection();
  const old = owner.load();
  const firstOptions = vi.mocked(apiGet).mock.calls[0]?.[1];
  await owner.load();
  expect(firstOptions?.signal?.aborted).toBe(true);
  complete(["old"]);
  expect(await old).toEqual([]);
  expect(owner.decode).toHaveBeenCalledTimes(1);
  expect(owner.state.data).toEqual(["latest"]);
});

it("ignores rejection after disposing the owner and completes the request channel", async () => {
  let reject!: (_error: Error) => void;
  vi.mocked(apiGet).mockReturnValue(
    new Promise((_resolve, fail) => {
      reject = fail;
    }),
  );
  const owner = collection();
  const pending = owner.load();
  owner.guard.dispose();
  reject(new Error("Late failure"));
  expect(await pending).toEqual([]);
  expect(owner.state.error).toBeNull();
  expect(owner.decode).not.toHaveBeenCalled();
});
