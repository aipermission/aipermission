import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { useCredentialInventory } from "./use-credential-inventory";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
});

it("loads decoded credential inventories without requesting unrelated project or detail resources", async () => {
  vi.mocked(apiGet).mockImplementation((path) =>
    Promise.resolve({ items: path === "/api/connectors" ? [{ kind: "ssh", label: "SSH", version: "0.2" }] : [inventoryTargetFixture()] }),
  );
  const { result } = renderHook(() => useCredentialInventory());
  await waitFor(() => expect(result.current.targets.state).toBe("ready"));
  expect(result.current.catalog.data[0]?.kind).toBe("ssh");
  expect(result.current.targets.data[0]?.id).toBe(inventoryTargetFixture().id);
  expect(
    vi
      .mocked(apiGet)
      .mock.calls.map(([path]) => path)
      .sort(),
  ).toEqual(["/api/connector-targets/inventory", "/api/connectors"]);
  await act(async () => {
    await result.current.refresh();
  });
  expect(apiGet).toHaveBeenCalledTimes(4);
});

it("aborts both channels when the credentials screen unmounts", async () => {
  vi.mocked(apiGet).mockImplementation(() => new Promise(() => {}));
  const { unmount } = renderHook(() => useCredentialInventory());
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  const signals = vi.mocked(apiGet).mock.calls.map(([, options]) => options?.signal);
  unmount();
  expect(signals.every((signal) => signal?.aborted)).toBe(true);
});
