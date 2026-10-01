import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, currentWorkspaceBinding } from "../../lib/api";
import { roleHistoryPageFixture, deferredRoleHistoryReply } from "./role-history-fixtures.test";
import { useRoleHistory } from "../../connectors/templates/postgres/role-lifecycle/use-role-history";

const workspace = "role-history-transport-workspace";
function json(value: unknown, binding: string | null = workspace) {
  return new Response(JSON.stringify(value), {
    headers: { ...(binding === null ? {} : { "X-AIPermission-Workspace": binding }), "X-AIPermission-Workspace-Changed": "true" },
  });
}
beforeEach(async () => {
  vi.stubGlobal("fetch", async () => json({}));
  await apiGet("/api/status");
});

it.each([null, "", "another-workspace"])("does not accept history under workspace %j", async (binding) => {
  vi.stubGlobal("fetch", async () => json(roleHistoryPageFixture(), binding));
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("error"));
  expect(result.current.page).toBeNull();
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it.each([
  ["presence", ""],
  ["presence", null],
  ["presence", "another-workspace"],
  ["cleanup", ""],
  ["cleanup", null],
  ["cleanup", "another-workspace"],
] as const)("retains uncertainty after %s succeeds with unowned reply %j", async (mode, binding) => {
  const initial = roleHistoryPageFixture();
  if (mode === "presence") initial.entries[0]!.record.status = "cleanup_intent";
  const observed = structuredClone(initial);
  observed.entries[0]!.record.status = mode === "presence" ? "provisioned" : "cleaned";
  observed.entries[0]!.record.generation = "d".repeat(32);
  const reload = deferredRoleHistoryReply();
  const fetch = vi.fn<typeof globalThis.fetch>();
  fetch.mockResolvedValueOnce(json(initial));
  fetch.mockResolvedValueOnce(
    json(
      {
        target_id: 1,
        evidence: mode === "presence" ? "exact_remote_identity_present" : "acknowledged_remote_cleanup",
        entry: observed.entries[0],
      },
      binding,
    ),
  );
  fetch.mockImplementationOnce(async () => (await reload.promise) as Response);
  vi.stubGlobal("fetch", fetch);
  const { result } = renderHook(() => useRoleHistory(1));
  await waitFor(() => expect(result.current.state).toBe("ready"));
  const entry = result.current.page!.entries[0]!;
  let decision!: Promise<void>;
  act(() => {
    decision = mode === "presence" ? result.current.reconcile(entry, 2) : result.current.cleanup(entry, 2, entry.record.intent.role_name);
  });
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
  expect(result.current.state).toBe("loading");
  expect(result.current.busy).toBe(true);
  await act(async () => {
    await result.current.refresh();
  });
  expect(fetch).toHaveBeenCalledTimes(3);
  await act(async () => {
    reload.resolve(json(observed));
    await decision;
  });
  expect(result.current.busy).toBe(false);
  expect(result.current.state).toBe("ready");
  expect(result.current.notice).toMatch(/^Decision outcome was not confirmed/);
  expect(result.current.page!.entries[0]!.record.status).toBe(mode === "presence" ? "provisioned" : "cleaned");
  expect(currentWorkspaceBinding()).toBe(workspace);
  expect(fetch).toHaveBeenCalledTimes(3);
  for (const [, options] of fetch.mock.calls) expect(new Headers(options?.headers).get("X-AIPermission-Workspace")).toBe(workspace);
});
