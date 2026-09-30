import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../../lib/api";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { connectorActionRequest, connectorApprovalFixture } from "../../../test/connector-action-fixtures";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { RedisConfirmDialog } from "./confirm-dialog";
import { RedisKeyBrowser } from "./key-browser";
import { useRedisBrowser } from "./use-redis-browser";
import { RedisValueWorkspace } from "./value-workspace";
import { mutationTestWorkspace, setupMutationRetryStorage } from "../../../test/connector-mutation-test-state";

setupMutationRetryStorage();

vi.mock("../../../lib/api", () => ({
  apiPost: vi.fn(),
  apiGet: vi.fn(async () => []),
  currentWorkspaceBinding: () => mutationTestWorkspace,
}));
const stored = new Map<string, string>();
const expiry = new Map<string, number>();

beforeEach(() => {
  vi.mocked(apiGet).mockReset().mockResolvedValue([]);
  stored.clear();
  stored.set("alpha", "Alpha value").set("beta", "Beta value").set("gamma", "Gamma value");
  expiry.clear();
  vi.mocked(apiPost)
    .mockReset()
    .mockImplementation(async (path, payload) => {
      expect(path).toBe("/api/connector-actions/local-run");
      const request = connectorActionRequest(payload);
      const input = request.input;
      const key = String(input.key || "");
      let output: unknown;
      if (request.action_name === "scan_keys") {
        const keys = [...stored.keys()].filter((item) => item.includes(String(input.pattern).replaceAll("*", "")));
        output = {
          keys: input.cursor === "0" ? keys.slice(0, 2) : keys.slice(2),
          next_cursor: input.cursor === "0" && keys.length > 2 ? "42" : "0",
        };
      } else if (request.action_name === "get_key") {
        output = { key, type: "string", value: stored.get(key), ttl_ms: expiry.get(key) ?? -1 };
      } else if (request.action_name === "expire_key") {
        expiry.set(key, Number(input.ttl_seconds) * 1000);
        output = { key };
      } else if (request.action_name === "set_string") {
        stored.set(key, String(input.value));
        if (Number(input.ttl_seconds) > 0) expiry.set(key, Number(input.ttl_seconds) * 1000);
        else expiry.delete(key);
        output = { key };
      } else if (request.action_name === "delete_keys") {
        for (const item of Array.isArray(input.keys) ? input.keys : []) stored.delete(String(item));
        output = { deleted: Array.isArray(input.keys) ? input.keys.length : 0 };
      } else throw new Error(`Unexpected action ${request.action_name}`);
      const response: ConnectorActionResponse = {
        request_id: 1,
        status: "completed",
        target_ref: "redis:1:1",
        connector_kind: "redis",
        action_name: request.action_name,
        retry_policy: {
          class: request.action_name === "scan_keys" || request.action_name === "get_key" ? "read_only" : "non_idempotent",
          guidance: "Inspect the result before retrying.",
        },
        output,
      };
      return response;
    });
});

function actionCalls() {
  return vi.mocked(apiPost).mock.calls.map(([, payload]) => connectorActionRequest(payload));
}

function Workspace({
  serverFamily = "redis",
  approvals = { state: "ready", data: [] },
}: {
  serverFamily?: string;
  approvals?: Parameters<typeof useRedisBrowser>[0]["approvals"];
}) {
  const browser = useRedisBrowser({
    target: { ref: "redis:1:1", connector_kind: "redis", config: { server_family: serverFamily } },
    approvals,
    session: { active: true, startedAt: "test-session" },
    onRefreshActivity: vi.fn(),
  });
  const styles = connectorConsoleTheme("dark");
  return (
    <>
      <RedisKeyBrowser browser={browser} styles={styles} />
      <RedisValueWorkspace browser={browser} styles={styles} />
      <RedisConfirmDialog
        value={browser.confirmDialog}
        theme="dark"
        product={browser.product}
        onClose={browser.closeConfirmDialog}
        onConfirm={browser.confirmPendingAction}
      />
    </>
  );
}

it.each(["completed", "failed", "approval_pending"] as const)("shows the latest matching %s action beside key controls", async (status) => {
  render(
    <Workspace
      approvals={{
        data: [
          connectorApprovalFixture({ target_ref: "redis:2:2", action_name: "unrelated_action" }),
          connectorApprovalFixture({ target_ref: "redis:1:1", action_name: "expire_key", status }),
        ],
      }}
    />,
  );
  await screen.findByTitle("alpha");
  expect(screen.getByText("expire_key")).toBeVisible();
  expect(screen.queryByText("unrelated_action")).not.toBeInTheDocument();
});

it("selects without reading values, paginates and searches through the native browser", async () => {
  const user = userEvent.setup();
  render(<Workspace />);
  await screen.findByTitle("alpha");
  await user.click(screen.getByRole("checkbox", { name: "Select alpha" }));
  expect(screen.getByRole("checkbox", { name: "Select alpha" })).toBeChecked();
  expect(vi.mocked(apiPost).mock.calls.map(([, payload]) => connectorActionRequest(payload).action_name)).toEqual(["scan_keys"]);
  await user.click(screen.getByRole("button", { name: "All" }));
  expect(screen.getByRole("checkbox", { name: "Select beta" })).toBeChecked();
  await user.click(screen.getByRole("button", { name: "None" }));
  expect(screen.getByRole("checkbox", { name: "Select alpha" })).not.toBeChecked();
  await user.click(screen.getByRole("button", { name: "More" }));
  await screen.findByTitle("gamma");
  expect(screen.getByRole("button", { name: "More" })).toBeDisabled();
  expect(apiPost).toHaveBeenCalledWith(
    expect.anything(),
    expect.objectContaining({ action_name: "scan_keys", input: expect.objectContaining({ cursor: "42" }) }),
    expect.anything(),
  );
  const search = screen.getByRole("textbox", { name: "Redis key scan pattern" });
  await user.clear(search);
  await user.type(search, "alpha");
  await user.click(screen.getByRole("button", { name: "Scan keys" }));
  await waitFor(() => expect(screen.queryByTitle("beta")).not.toBeInTheDocument());
  expect(screen.getByTitle("alpha")).toBeVisible();
  expect(apiPost).toHaveBeenLastCalledWith(
    expect.anything(),
    expect.objectContaining({ action_name: "scan_keys", input: expect.objectContaining({ pattern: "*alpha*", cursor: "0" }) }),
    expect.anything(),
  );
  await user.click(screen.getByRole("button", { name: "Refresh keys" }));
  expect(apiPost).toHaveBeenCalledTimes(4);
});

it("removes selected keys only after the destructive dialog is confirmed", async () => {
  const user = userEvent.setup();
  render(<Workspace />);
  await screen.findByTitle("alpha");
  await user.click(screen.getByRole("button", { name: "All" }));
  await user.click(screen.getByRole("button", { name: "Delete 2" }));
  expect(screen.getByRole("dialog")).toHaveAccessibleName("Delete 2 Redis keys");
  expect(stored.has("alpha")).toBe(true);
  expect(stored.has("beta")).toBe(true);
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(actionCalls().filter((item) => item.action_name === "delete_keys")).toEqual([]);
  expect([...stored.keys()]).toEqual(["alpha", "beta", "gamma"]);
  await user.click(screen.getByRole("button", { name: "Delete 2" }));
  await user.click(screen.getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(screen.queryByTitle("alpha")).not.toBeInTheDocument());
  expect(screen.queryByTitle("beta")).not.toBeInTheDocument();
  expect(stored.has("gamma")).toBe(true);
  expect(actionCalls().filter((item) => item.action_name === "delete_keys")).toHaveLength(1);
  expect(screen.getByText("No keys loaded. Scan to browse this database.")).toBeVisible();
  expect(apiPost).toHaveBeenCalledWith(
    expect.anything(),
    expect.objectContaining({ action_name: "delete_keys", input: { keys: ["alpha", "beta"] } }),
    expect.anything(),
  );
});

it("disables destructive controls while a discovered write remains pending", async () => {
  vi.mocked(apiGet).mockResolvedValueOnce([connectorApprovalFixture({ target_ref: "redis:1:1", action_name: "set_string" })]);
  const user = userEvent.setup();
  render(<Workspace />);
  await screen.findByTitle("alpha");
  await user.click(screen.getByRole("checkbox", { name: "Select alpha" }));
  expect(screen.getByRole("button", { name: "Delete 1" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Delete 1" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(actionCalls().filter((item) => item.action_name === "delete_keys")).toEqual([]);
});

it("edits a string and TTL through the real confirmation and reload flow", async () => {
  const user = userEvent.setup();
  render(<Workspace />);
  await user.click(await screen.findByTitle("alpha"));
  const value = await screen.findByRole("textbox", { name: "String value" });
  await waitFor(() => expect(value).toHaveValue("Alpha value"));
  expect(screen.getByTitle("alpha").closest("button")).toHaveAttribute("aria-pressed", "true");
  await user.type(screen.getByRole("textbox", { name: "TTL seconds" }), "60");
  await user.click(screen.getByRole("button", { name: "Save TTL" }));
  expect(screen.getByRole("dialog")).toHaveAccessibleName("Update Redis TTL");
  expect(actionCalls().filter((item) => item.action_name === "expire_key")).toEqual([]);
  expect(expiry.has("alpha")).toBe(false);
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(screen.getByRole("textbox", { name: "TTL seconds" })).toHaveValue("60");
  expect(expiry.get("alpha")).toBe(60000);
  expect(actionCalls().slice(-2)).toEqual([
    expect.objectContaining({ action_name: "expire_key", input: { key: "alpha", ttl_seconds: 60 } }),
    expect.objectContaining({ action_name: "get_key", input: { key: "alpha", limit: 250, max_bytes: 262144 } }),
  ]);
  await user.clear(value);
  await user.type(value, "Updated value");
  await user.click(screen.getByRole("button", { name: "Save string" }));
  expect(stored.get("alpha")).toBe("Alpha value");
  expect(actionCalls().filter((item) => item.action_name === "set_string")).toEqual([]);
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(stored.get("alpha")).toBe("Updated value");
  expect(expiry.get("alpha")).toBe(60000);
  expect(actionCalls().slice(-2)).toEqual([
    expect.objectContaining({ action_name: "set_string", input: { key: "alpha", value: "Updated value", ttl_seconds: 60 } }),
    expect.objectContaining({ action_name: "get_key" }),
  ]);
  expect(screen.getByRole("textbox", { name: "String value" })).toHaveValue("Updated value");
  await user.click(screen.getByRole("button", { name: "Raw JSON" }));
  expect(screen.getByText(/"value": "Updated value"/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Value" }));
  await user.click(screen.getByRole("button", { name: "Reload key" }));
  expect(await screen.findByRole("textbox", { name: "String value" })).toHaveValue("Updated value");
});

it.each(["redis", "valkey"])("creates a %s key only after nonempty validation and confirmation", async (serverFamily) => {
  const user = userEvent.setup();
  render(<Workspace serverFamily={serverFamily} />);
  await screen.findByTitle("alpha");
  await user.click(screen.getByRole("button", { name: "New" }));
  await user.type(screen.getByRole("textbox", { name: "New key name" }), "cache:new");
  await user.click(screen.getByRole("button", { name: "Create key" }));
  expect(screen.getByText("Value is required.")).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(stored.has("cache:new")).toBe(false);
  await user.type(screen.getByRole("textbox", { name: "String value" }), "New value");
  await user.click(screen.getByRole("button", { name: "Create key" }));
  expect(screen.getByRole("dialog")).toHaveAccessibleName(`Create ${serverFamily === "valkey" ? "Valkey" : "Redis"} string key`);
  expect(stored.has("cache:new")).toBe(false);
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(screen.getByTitle("cache:new")).toBeVisible();
  expect(screen.getByRole("textbox", { name: "String value" })).toHaveValue("New value");
  expect(stored.get("cache:new")).toBe("New value");
});
