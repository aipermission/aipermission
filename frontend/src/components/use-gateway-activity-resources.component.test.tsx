import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../lib/api";
import { useGatewayActivityResources } from "./use-gateway-activity-resources";
import type { ConnectorApproval } from "../lib/gateway-contracts/security-contracts";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiPost).mockReset();
});

const pendingApproval: ConnectorApproval = {
  id: 12,
  target_id: 1,
  target_name: "fixture",
  target_ref: "ssh:1:1",
  profile_id: 1,
  profile_label: "default",
  connector_kind: "ssh",
  action_name: "exec",
  status: "approval_pending",
  approval_context_hash: "approval-context",
  retry_policy: { class: "non_idempotent", guidance: "Inspect before retrying." },
  created_at: "2026-09-16T00:00:00Z",
};

const message = { id: 3, token_id: 1, runtime_id: 7, direction: "ai_to_user", message: "note", created_at: "2026-09-26" };

it("refreshes validated approvals after running an action and marks runtime messages read", async () => {
  vi.mocked(apiGet).mockImplementation(async (path) => {
    if (path === "/api/connector-action-approvals") return [pendingApproval];
    if (path === "/api/messages") return [message];
    throw new Error(`Unexpected GET ${path}`);
  });
  vi.mocked(apiPost).mockResolvedValue({ ...pendingApproval, status: "completed" });
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadConnectorActionApprovals());
  expect(result.current.connectorActionApprovals).toMatchObject({ state: "ready", data: [pendingApproval] });

  await act(async () => result.current.runConnectorActionApproval(pendingApproval, "reviewed"));
  expect(apiPost).toHaveBeenCalledWith("/api/connector-action-approvals/12/run", {
    user_note: "reviewed",
    approval_context_hash: "approval-context",
  });
  expect(apiGet).toHaveBeenCalledTimes(2);

  await act(async () => result.current.markRuntimeMessagesRead("7"));
  expect(apiPost).toHaveBeenCalledWith("/api/messages/read", { runtime_id: 7 });
  expect(result.current.messages).toMatchObject({ state: "ready", data: [message] });
});

it("rejects malformed pending approvals without exposing them as ready data", async () => {
  vi.mocked(apiGet).mockResolvedValue([{ status: "pending" }]);
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadConnectorActionApprovals());

  expect(result.current.connectorActionApprovals).toMatchObject({ state: "error", data: [] });
  expect(result.current.connectorActionApprovals.error).toMatch(/Invalid connector approvals response/);
});

it("retains the last validated message snapshot after a malformed response", async () => {
  vi.mocked(apiGet)
    .mockResolvedValueOnce([message])
    .mockResolvedValueOnce([{ ...message, token_id: {} }]);
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));
  await act(async () => result.current.loadMessages());
  await act(async () => result.current.loadMessages());
  expect(result.current.messages).toMatchObject({ state: "error", data: [message], error: "Invalid runtime messages response." });
});

it("ignores stale message responses and aborts an active freshness read on unmount", async () => {
  vi.mocked(apiGet).mockResolvedValue([message]);
  const { result, unmount } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => false }));
  await act(async () => result.current.loadMessages(2));
  expect(result.current.messages).toEqual({ state: "loading", data: [], error: null });
  let finish: ((_value: unknown) => void) | undefined;
  vi.mocked(apiGet).mockReturnValue(
    new Promise<unknown>((resolve) => {
      finish = resolve;
    }),
  );
  let load: Promise<void> | undefined;
  act(() => {
    load = result.current.loadBackupFreshness();
  });
  const options = vi.mocked(apiGet).mock.calls.at(-1)?.[1];
  const signal = options && "signal" in options ? options.signal : undefined;
  if (!(signal instanceof AbortSignal)) throw new Error("Freshness request is missing an abort signal");
  expect(signal.aborted).toBe(false);
  unmount();
  expect(signal.aborted).toBe(true);
  if (!finish) throw new Error("Freshness request did not start");
  finish({ items: [{ provider_id: 2 }], check_errors: [] });
  await load;
  expect(result.current.backupFreshness.data).toEqual([]);
});

it("keeps the last approval snapshot through a transient failure and recovers", async () => {
  vi.mocked(apiGet).mockResolvedValueOnce([pendingApproval]).mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce([]);
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadConnectorActionApprovals());
  await act(async () => result.current.loadConnectorActionApprovals());
  expect(result.current.connectorActionApprovals).toEqual({ state: "error", data: [pendingApproval], error: "offline" });

  await act(async () => result.current.loadConnectorActionApprovals());
  expect(result.current.connectorActionApprovals).toEqual({ state: "ready", data: [], error: null });
});

it("declines approvals, refreshes messages, and preserves freshness data on failure", async () => {
  vi.mocked(apiGet).mockImplementation(async (path) => {
    if (path === "/api/connector-action-approvals") return [];
    if (path === "/api/messages") return [{ ...message, id: 4 }];
    if (path === "/api/backup/freshness") return { items: [{ provider_id: 2 }], check_errors: [] };
    throw new Error(`Unexpected GET ${path}`);
  });
  vi.mocked(apiPost).mockResolvedValue({ ...pendingApproval, status: "declined" });
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadMessages(1));
  await act(async () => result.current.loadBackupFreshness());
  await act(async () => result.current.declineConnectorActionApproval(pendingApproval, "not now"));
  expect(apiPost).toHaveBeenCalledWith("/api/connector-action-approvals/12/decline", {
    user_note: "not now",
    approval_context_hash: "approval-context",
  });
  expect(result.current.messages.data).toEqual([{ ...message, id: 4 }]);
  expect(result.current.backupFreshness.data).toEqual([{ provider_id: 2 }]);

  vi.mocked(apiGet).mockRejectedValue(new Error("provider offline"));
  await act(async () => result.current.loadBackupFreshness());
  expect(result.current.backupFreshness).toMatchObject({ state: "error", data: [{ provider_id: 2 }], error: "provider offline" });
});

it("refreshes approvals after a failed run before surfacing the error", async () => {
  vi.mocked(apiPost).mockRejectedValue(new Error("execution failed"));
  vi.mocked(apiGet).mockResolvedValue([]);
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));
  await expect(act(async () => result.current.runConnectorActionApproval(pendingApproval))).rejects.toThrow("execution failed");
  expect(apiGet).toHaveBeenCalledWith("/api/connector-action-approvals", expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

it("rejects run and decline responses that do not match the displayed decision", async () => {
  vi.mocked(apiGet).mockResolvedValue([]);
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  vi.mocked(apiPost).mockResolvedValueOnce({ ...pendingApproval, id: 99, status: "completed", approval_context_hash: "" });
  await expect(act(async () => result.current.runConnectorActionApproval(pendingApproval))).rejects.toThrow(/Invalid connector approval/);

  vi.mocked(apiPost).mockResolvedValueOnce({ ...pendingApproval, status: "approval_pending" });
  await expect(act(async () => result.current.runConnectorActionApproval(pendingApproval))).rejects.toThrow(/Invalid connector approval/);

  vi.mocked(apiPost).mockResolvedValueOnce({ ...pendingApproval, status: "completed", approval_context_hash: "" });
  await expect(act(async () => result.current.declineConnectorActionApproval(pendingApproval))).rejects.toThrow(
    /Invalid connector approval/,
  );
});
