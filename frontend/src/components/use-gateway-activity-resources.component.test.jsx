import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../lib/api";
import { useGatewayActivityResources } from "./use-gateway-activity-resources";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
});

const pendingApproval = {
  id: 12,
  target_id: 1,
  target_name: "fixture",
  target_ref: "ssh:1:1",
  profile_id: 1,
  profile_label: "default",
  connector_kind: "ssh",
  action_name: "exec",
  status: "approval_pending",
  retry_policy: { class: "non_idempotent", guidance: "Inspect before retrying." },
  created_at: "2026-09-16T00:00:00Z",
};

it("refreshes validated approvals after running an action and marks runtime messages read", async () => {
  apiGet.mockImplementation(async (path) => {
    if (path === "/api/connector-action-approvals") return [pendingApproval];
    if (path === "/api/messages") return [{ id: 3, runtime_id: 7 }];
    throw new Error(`Unexpected GET ${path}`);
  });
  apiPost.mockResolvedValue({ ...pendingApproval, status: "completed" });
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadConnectorActionApprovals());
  expect(result.current.connectorActionApprovals).toMatchObject({ state: "ready", data: [pendingApproval] });

  await act(async () => result.current.runConnectorActionApproval(12, "reviewed"));
  expect(apiPost).toHaveBeenCalledWith("/api/connector-action-approvals/12/run", { user_note: "reviewed" });
  expect(apiGet).toHaveBeenCalledTimes(2);

  await act(async () => result.current.markRuntimeMessagesRead("7"));
  expect(apiPost).toHaveBeenCalledWith("/api/messages/read", { runtime_id: 7 });
  expect(result.current.messages).toMatchObject({ state: "ready", data: [{ id: 3, runtime_id: 7 }] });
});

it("rejects malformed pending approvals without exposing them as ready data", async () => {
  apiGet.mockResolvedValue([{ status: "pending" }]);
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadConnectorActionApprovals());

  expect(result.current.connectorActionApprovals).toMatchObject({ state: "error", data: [] });
  expect(result.current.connectorActionApprovals.error).toMatch(/Invalid connector approvals response/);
});
