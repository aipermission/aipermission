import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../api";
import { listLocalActionRetryEntries } from "../local-action-retry";
import { setupMutationRetryStorage, mutationTestWorkspace } from "../../test/connector-mutation-test-state";
import { scopedUICookieName } from "../ui-cookie";

setupMutationRetryStorage();
beforeEach(async () => {
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response("{}", {
        headers: { "X-AIPermission-Workspace": mutationTestWorkspace, "X-AIPermission-Workspace-Changed": "true" },
      }),
  );
  await apiGet("/api/status");
});

it("preserves the same mutation key after a gateway 5xx without a trustworthy outcome", async () => {
  const keys: string[] = [];
  const body = { target_ref: "example:3:11", action_name: "example_action", input: {}, reason: "uncertain gateway failure" };
  const fetch = vi.fn(async (_url: unknown, options?: RequestInit) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    return new Response('{"error":"synthetic gateway failure"}', { status: 503 });
  });
  vi.stubGlobal("fetch", fetch);
  for (let attempt = 0; attempt < 2; attempt++)
    await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toMatchObject({ status: 503 });
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ key: keys[0], state: "pending" })]);
});

it("sends the scoped CSRF token with a workspace-bound mutation", async () => {
  const name = scopedUICookieName("aipermission_csrf");
  document.cookie = `${name}=synthetic-csrf; path=/`;
  const fetch = vi.fn(async () => new Response('{"ok":true}'));
  vi.stubGlobal("fetch", fetch);
  try {
    await expect(apiPost("/api/settings", { retention_days: 2 })).resolves.toEqual({ ok: true });
    expect(fetch).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-CSRF": "synthetic-csrf",
          "X-AIPermission-Workspace": mutationTestWorkspace,
        },
      }),
    );
    expect(await listLocalActionRetryEntries()).toEqual([]);
  } finally {
    document.cookie = `${name}=; Max-Age=0; path=/`;
  }
});
