import { act, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { LocalActionRetryPanel } from "../../components/settings/local-action-retry-panel";
import { ServerRequestReconciliation } from "../../components/settings/server-request-reconciliation";
import { useConnectorMutationOwnership } from "../../connectors/templates/_shared/use-connector-mutation-ownership";
import { apiGet, apiPost } from "../../lib/api";
import { listLocalActionRetryEntries, localActionRetryLedgerChangedEvent } from "../../lib/local-action-retry";
import { reconcileVerifiedServerRequest, reconciledRequests } from "../../lib/local-action-retry/reconciliations";
import { connectorApprovalFixture } from "../connector-action-fixtures";
import { mutationObservationQueue, mutationTestWorkspace, setupMutationRetryStorage } from "../connector-mutation-test-state";

const targetRef = "example:1:1";
const actions = ["mutate"];
const remote = connectorApprovalFixture({
  id: 71,
  target_id: 1,
  profile_id: 1,
  target_ref: targetRef,
  action_name: actions[0],
  status: "outcome_unknown",
});
const observations = mutationObservationQueue();
const scope = { key: mutationTestWorkspace };

function jsonResponse(data: unknown, workspaceID = mutationTestWorkspace) {
  return new Response(JSON.stringify(data), {
    headers: {
      "Content-Type": "application/json",
      "X-AIPermission-Workspace": workspaceID,
      "X-AIPermission-Workspace-Changed": "true",
    },
  });
}

function gatewayResponse(url: string) {
  const path = new URL(url).pathname;
  if (path === "/api/status") return jsonResponse({});
  if (path === "/api/connector-action-approvals/71") return jsonResponse(remote);
  if (path === "/api/connector-action-approvals") return jsonResponse([remote]);
  throw new Error(`Unexpected fixture API request: ${path}`);
}

async function openServerConfirmation(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "Reconcile server request 71" }));
  return within(screen.getByRole("dialog")).getByRole("button", { name: "Reconcile server request" });
}

function deferredResponse() {
  let finish!: (_response: Response) => void;
  const response = new Promise<Response>((resolve) => {
    finish = resolve;
  });
  return { response, finish };
}
setupMutationRetryStorage();
beforeEach(async () => {
  observations.clear();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      const path = new URL(url).pathname;
      if (options?.method === "POST") {
        if (path !== "/api/connector-actions/local-run") throw new Error("Unexpected fixture mutation");
        throw new TypeError("POST reply lost");
      }
      return gatewayResponse(url);
    }),
  );
  await apiGet("/api/status");
});

it("recovers a lost POST reply through explicit server identity reconciliation without guessing its ledger identity", async () => {
  const user = userEvent.setup();
  await expect(
    apiPost(
      "/api/connector-actions/local-run",
      { target_ref: targetRef, action_name: actions[0], input: {}, reason: "lost reply fixture" },
      { exclusiveMutationActions: actions },
    ),
  ).rejects.toThrow("POST reply lost");
  const [entry] = await listLocalActionRetryEntries();
  expect(entry).not.toHaveProperty("request_id");
  const hook = renderHook(() => useConnectorMutationOwnership(targetRef, actions, "ready", apiGet, observations.schedule));
  const panel = render(<LocalActionRetryPanel />);
  await waitFor(() => expect(hook.result.current.locked).toBe(true));
  await user.click(await screen.findByRole("button", { name: "Mark retry identity reconciled" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Mark reconciled" }));
  await waitFor(async () => expect(await listLocalActionRetryEntries()).toEqual([]));
  await user.click(await screen.findByRole("button", { name: "Reconcile server request 71" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Reconcile server request" }));
  await act(async () => observations.advance());
  await waitFor(() => expect(hook.result.current.locked).toBe(false));
  hook.unmount();
  panel.unmount();
  const remounted = renderHook(() => useConnectorMutationOwnership(targetRef, actions, "ready", apiGet, observations.schedule));
  await waitFor(() => expect(remounted.result.current.locked).toBe(false));
  expect(remote.status).toBe("outcome_unknown");
  expect(vi.mocked(fetch).mock.calls.filter(([, options]) => options?.method === "POST")).toHaveLength(1);
});

it("reconciles a server-only request with no local ledger and persists the decision for other observers", async () => {
  const user = userEvent.setup();
  render(<ServerRequestReconciliation />);
  const confirm = await openServerConfirmation(user);
  expect(await reconciledRequests(scope)).toEqual([]);
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }));
  expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).endsWith("/71"))).toBe(false);
  expect(confirm).not.toBeInTheDocument();
  await user.click(await openServerConfirmation(user));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(await listLocalActionRetryEntries()).toEqual([]);
  expect(await reconciledRequests(scope)).toMatchObject([{ request_id: 71, target_ref: targetRef, action_name: actions[0] }]);
  const detailCall = vi.mocked(fetch).mock.calls.find(([url]) => String(url).endsWith("/71"));
  expect(detailCall?.[1]).toMatchObject({ headers: { "X-AIPermission-Workspace": mutationTestWorkspace } });
});

it("keeps an unacknowledged local attempt protected when the operator reconciles the server request first", async () => {
  const user = userEvent.setup();
  await expect(
    apiPost(
      "/api/connector-actions/local-run",
      { target_ref: targetRef, action_name: actions[0], input: {}, reason: "server-first fixture" },
      { exclusiveMutationActions: actions },
    ),
  ).rejects.toThrow("POST reply lost");
  const entries = await listLocalActionRetryEntries();
  expect(entries[0]).not.toHaveProperty("request_id");
  const hook = renderHook(() => useConnectorMutationOwnership(targetRef, actions, "ready", apiGet, observations.schedule));
  render(<LocalActionRetryPanel />);
  await waitFor(() => expect(hook.result.current.locked).toBe(true));
  await user.click(await openServerConfirmation(user));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  await act(async () => observations.advance());
  expect(await listLocalActionRetryEntries()).toEqual(entries);
  expect(hook.result.current.locked).toBe(true);
  await user.click(screen.getByRole("button", { name: "Mark retry identity reconciled" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Mark reconciled" }));
  await act(async () => observations.advance());
  await waitFor(() => expect(hook.result.current.locked).toBe(false));
});

it("recovers the 101st unknown request by exact ID after the newest 100 remain server-side unknown", async () => {
  const user = userEvent.setup();
  const newest = Array.from({ length: 100 }, (_, index) => ({ ...remote, id: index + 2 }));
  for (const item of newest) await reconcileVerifiedServerRequest(scope, item);
  vi.mocked(fetch).mockImplementation(async (url) => jsonResponse(String(url).endsWith("/1") ? { ...remote, id: 1 } : newest));
  render(<ServerRequestReconciliation />);
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled());
  expect(screen.queryByRole("button", { name: /Reconcile server request/ })).not.toBeInTheDocument();
  await user.type(screen.getByRole("textbox", { name: "Server request ID" }), "1");
  await user.click(screen.getByRole("button", { name: "Find server request" }));
  await user.click(await screen.findByRole("button", { name: "Reconcile server request 1" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Reconcile server request" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(await reconciledRequests(scope)).toHaveLength(101);
  expect(newest.every((item) => item.status === "outcome_unknown")).toBe(true);
  expect(await listLocalActionRetryEntries()).toEqual([]);
  expect(vi.mocked(fetch).mock.calls.filter(([url]) => String(url).endsWith("/1"))).toHaveLength(3);
});

it.each(["0", "-1", "1.5", "9007199254740992", "not-an-id"])("rejects invalid exact request ID %s without a lookup", async (value) => {
  const user = userEvent.setup();
  render(<ServerRequestReconciliation />);
  await screen.findByRole("button", { name: "Reconcile server request 71" });
  const reads = vi.mocked(fetch).mock.calls.length;
  await user.type(screen.getByRole("textbox", { name: "Server request ID" }), value);
  await user.click(screen.getByRole("button", { name: "Find server request" }));
  expect(await screen.findByText("Enter a positive request ID.")).toBeVisible();
  expect(vi.mocked(fetch)).toHaveBeenCalledTimes(reads);
});

it("does not trigger repeated server reads when retry storage is unavailable", async () => {
  const original = indexedDB;
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const changed = vi.fn();
  window.addEventListener(localActionRetryLedgerChangedEvent, changed);
  vi.stubGlobal("indexedDB", undefined);
  const view = render(<ServerRequestReconciliation />);
  try {
    expect(await screen.findByText(/Secure retry storage is unavailable/)).toBeVisible();
    await act(async () => {
      for (let index = 0; index < 10; index++) await Promise.resolve();
    });
    expect(vi.mocked(fetch).mock.calls.filter(([url]) => String(url).includes("/connector-action-approvals"))).toHaveLength(1);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(changed).not.toHaveBeenCalled();
  } finally {
    view.unmount();
    window.removeEventListener(localActionRetryLedgerChangedEvent, changed);
    warn.mockRestore();
    vi.stubGlobal("indexedDB", original);
  }
});

it.each([
  ["running", { status: "running" }],
  ["approval pending", { status: "approval_pending" }],
  ["completed", { status: "completed" }],
  ["wrong ID", { id: 72 }],
  ["wrong target", { target_ref: "example:1:2" }],
  ["wrong action", { action_name: "another_action" }],
])("refuses operator reconciliation after the exact request becomes %s", async (_label, change) => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => (String(url).endsWith("/71") ? jsonResponse({ ...remote, ...change }) : gatewayResponse(url))),
  );
  render(<ServerRequestReconciliation />);
  await user.click(await openServerConfirmation(user));
  expect(await within(screen.getByRole("dialog")).findByText(/Only a verified unknown|identity/i)).toBeVisible();
  expect(await reconciledRequests(scope)).toEqual([]);
  expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Reconcile server request" })).toBeEnabled();
});

it("does not persist a proof after a workspace mismatch or a lost exact read", async () => {
  const user = userEvent.setup();
  let mismatch = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (!String(url).endsWith("/71")) return gatewayResponse(url);
      if (mismatch) return jsonResponse(remote, "other-workspace");
      throw new TypeError("exact read lost");
    }),
  );
  render(<ServerRequestReconciliation />);
  await user.click(await openServerConfirmation(user));
  expect(await within(screen.getByRole("dialog")).findByText(/workspace binding mismatch/)).toBeVisible();
  mismatch = false;
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Reconcile server request" }));
  expect(await within(screen.getByRole("dialog")).findByText("exact read lost")).toBeVisible();
  expect(await reconciledRequests(scope)).toEqual([]);
});

it("keeps an older list completion from replacing the newer scoped result", async () => {
  const user = userEvent.setup();
  const initial = deferredResponse();
  let reads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => (++reads === 1 ? initial.response : jsonResponse([{ ...remote, id: 72 }]))),
  );
  render(<ServerRequestReconciliation />);
  await user.click(screen.getByRole("button", { name: "Refresh server requests" }));
  expect(await screen.findByRole("button", { name: "Reconcile server request 72" })).toBeVisible();
  await act(async () => initial.finish(jsonResponse([remote])));
  expect(screen.queryByRole("button", { name: "Reconcile server request 71" })).not.toBeInTheDocument();
});

it("ignores an exact read after unmount without recording any reconciliation", async () => {
  const user = userEvent.setup();
  const pending = deferredResponse();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => (String(url).endsWith("/71") ? pending.response : gatewayResponse(url))),
  );
  const view = render(<ServerRequestReconciliation />);
  await user.click(await openServerConfirmation(user));
  expect(screen.getByRole("button", { name: "Verifying request..." })).toBeDisabled();
  view.unmount();
  await act(async () => pending.finish(jsonResponse(remote)));
  expect(await reconciledRequests(scope)).toEqual([]);
});

it("does not carry old rows or confirmation into a switched workspace", async () => {
  const user = userEvent.setup();
  const pending = deferredResponse();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => (String(url).endsWith("/71") ? pending.response : gatewayResponse(url))),
  );
  const view = render(<ServerRequestReconciliation />);
  await user.click(await openServerConfirmation(user));
  vi.mocked(fetch).mockImplementationOnce(async () => jsonResponse({}, "other-workspace"));
  await apiGet("/api/status");
  view.rerender(<ServerRequestReconciliation />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reconcile server request 71" })).not.toBeInTheDocument();
  await act(async () => pending.finish(jsonResponse(remote)));
  expect(await reconciledRequests(scope)).toEqual([]);
  expect(await reconciledRequests({ key: "other-workspace" })).toEqual([]);
});

it("shows a server lookup error without treating it as a malformed local ledger", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      throw new Error("server requests unavailable");
    }),
  );
  render(<ServerRequestReconciliation />);
  expect(await screen.findByText("server requests unavailable")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Reset ledger" })).not.toBeInTheDocument();
});
