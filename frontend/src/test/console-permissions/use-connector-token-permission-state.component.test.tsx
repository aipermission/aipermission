import { act, fireEvent, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet as realGet } from "../../lib/api";
import { useConnectorTokenPermissionState } from "../../components/console/use-connector-token-permission-state";
import type { ConnectorTokenPermissionOptions } from "../../components/console/use-connector-token-permission-state";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));
const get = vi.mocked(realGet);
const target = { connector_kind: "fixture", target_id: 7, profile_id: 11, project_id: 3 };
const token = { id: 5, name: "Agent" };

function renderPermissions(overrides: Partial<ConnectorTokenPermissionOptions> = {}) {
  const options: ConnectorTokenPermissionOptions = {
    selectedTarget: target,
    targets: { data: [target] },
    tokens: { data: [token] },
    loadAllConnectorPermissions: vi.fn().mockResolvedValue({}),
    loadConnectorActions: vi.fn().mockResolvedValue([]),
    replaceTokenConnectorPermissions: vi.fn().mockResolvedValue([]),
    ...overrides,
  };
  return { options, ...renderHook(() => useConnectorTokenPermissionState(options)) };
}

beforeEach(() => {
  window.localStorage.clear();
  get.mockReset();
  get.mockResolvedValue({
    items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled: true }],
    revision: "r1",
  });
});

it("uses only a validated project scope snapshot for token visibility", async () => {
  const { result } = renderPermissions();
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  expect(result.current.projectEnabledForToken(5)).toBe(true);
});

it("keeps project visibility unavailable when its revision is malformed", async () => {
  get.mockResolvedValue({ items: [], revision: 9 });
  const { result } = renderPermissions();
  await waitFor(() => expect(result.current.projectScopeError).toContain("Invalid project scope"));
  expect(result.current.projectScopeReadyForToken(5)).toBe(false);
});

it("serializes permission mutations while the current write is pending", async () => {
  let finish!: (_value: []) => void;
  const write = vi.fn().mockReturnValue(
    new Promise<[]>((resolve) => {
      finish = resolve;
    }),
  );
  const { result } = renderPermissions({ replaceTokenConnectorPermissions: write });
  await waitFor(() => expect(result.current.projectScopeReadyForToken(5)).toBe(true));
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.setConnectorRule(token, 11, { name: "read" }, "always_run");
    void result.current.setConnectorRule(token, 11, { name: "write" }, "approval_required");
  });
  expect(write).toHaveBeenCalledOnce();
  await act(async () => {
    finish([]);
    await pending;
  });
  expect(result.current.savingKey).toBe("");
});

it("dismisses the compact panel outside or on Escape and restores trigger focus", async () => {
  const { result, unmount } = renderPermissions();
  const panel = document.createElement("section");
  const trigger = document.createElement("button");
  document.body.append(panel, trigger);
  try {
    result.current.compactPanelRef.current = panel;
    result.current.tokenTriggerRef.current = trigger;
    act(() => result.current.setOpenTokenID(5));
    fireEvent.pointerDown(panel);
    fireEvent.keyDown(window, { key: "ArrowDown" });
    expect(result.current.openTokenID).toBe(5);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(result.current.openTokenID).toBeNull();
    await waitFor(() => expect(document.activeElement).toBe(trigger));
    act(() => result.current.setOpenTokenID(5));
    fireEvent.pointerDown(document.body);
    expect(result.current.openTokenID).toBeNull();
    act(() => result.current.setOpenTokenID(5));
    unmount();
    trigger.blur();
    fireEvent.keyDown(window, { key: "Escape" });
    await act(async () => {
      await Promise.resolve();
    });
    expect(document.activeElement).not.toBe(trigger);
  } finally {
    panel.remove();
    trigger.remove();
  }
});
