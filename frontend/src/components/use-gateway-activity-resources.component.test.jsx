import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../lib/api";
import { useGatewayActivityResources } from "./use-gateway-activity-resources";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
});

it("refreshes validated approvals after running an action and marks runtime messages read", async () => {
  apiGet.mockImplementation(async (path) => {
    if (path === "/api/connector-action-approvals") return [{ id: 12, status: "pending" }];
    if (path === "/api/messages") return [{ id: 3, runtime_id: 7 }];
    throw new Error(`Unexpected GET ${path}`);
  });
  apiPost.mockResolvedValue({ status: "completed" });
  const { result } = renderHook(() => useGatewayActivityResources({ pollIsCurrent: () => true }));

  await act(async () => result.current.loadConnectorActionApprovals());
  expect(result.current.connectorActionApprovals).toMatchObject({ state: "ready", data: [{ id: 12, status: "pending" }] });

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
