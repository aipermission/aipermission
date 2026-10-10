import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { APIError } from "../../lib/errors";
import { listLocalActionRetryEntries, resolveLocalActionRetryEntry } from "../../lib/local-action-retry";
import { reconcileVerifiedServerRequest } from "../../lib/local-action-retry/reconciliations";
import { useMailWorkspace } from "../../connectors/templates/mail/use-mail-workspace";
import { useMailCompose } from "../../connectors/templates/mail/use-mail-compose";
import { MailActionFailure } from "../../connectors/templates/mail/use-mail-action-runner";
import { connectorActionFixture, connectorApprovalFixture } from "../connector-action-fixtures";
import { mutationObservationQueue, mutationTestWorkspace, setupMutationRetryStorage } from "../connector-mutation-test-state";

const observations = mutationObservationQueue();
vi.mock("../../connectors/templates/_shared/observation-scheduler", () => ({
  scheduleObservation: (observe: () => Promise<void>, delay: number) => observations.schedule(observe, delay),
}));
const fields = { to: ["synthetic@example.invalid"], cc: [], bcc: [], subject: "Report", text_body: "Ready", html_body: "" };
const target = { ref: "mail:1:1", public: { imap_enabled: false, smtp_auth_mode: "separate" } };
const remote = connectorApprovalFixture({ id: 71, target_ref: target.ref, action_name: "send_message", status: "outcome_unknown" });
function submissionFailure(status: "failed" | "outcome_unknown") {
  const message = "SMTP submission failed";
  const item = connectorActionFixture({ target_ref: target.ref, action_name: "send_message", status, error: message });
  return new MailActionFailure(message, item, { actionName: "send_message", item, summary: message });
}
function json(value: unknown) {
  return new Response(JSON.stringify(value), { headers: { "X-AIPermission-Workspace": mutationTestWorkspace } });
}
setupMutationRetryStorage();
beforeEach(() => observations.clear());

it("tracks the newly dispatched uncertain message after reconciling a previous attempt", async () => {
  const posts: Record<string, unknown>[] = [];
  vi.stubGlobal("fetch", async (url: string, options?: RequestInit) => {
    if (options?.method === "POST") {
      posts.push(JSON.parse(String(options.body)));
      return json(
        connectorActionFixture({
          request_id: 70 + posts.length,
          target_ref: target.ref,
          action_name: "send_message",
          status: "outcome_unknown",
          error: "SMTP DATA reply lost",
          output: { submission_status: "submission_unknown", message_id: `<attempt-${posts.length}@example.invalid>` },
        }),
      );
    }
    const action = new URL(url).searchParams.get("action_name");
    return json(action === "send_message" && posts.length ? [{ ...remote, id: 70 + posts.length }] : action ? [] : {});
  });
  await apiGet("/api/status");
  const { result } = renderHook(() => useMailWorkspace({ target, approvals: { state: "ready", data: [] } }));
  await waitFor(() => expect(result.current.outboundPending).toBe(false));
  act(() => result.current.openCompose());
  await act(async () => result.current.compose.submitMessage(fields));
  expect(result.current.compose.compose.submissionUnknown?.messageID).toBe("<attempt-1@example.invalid>");
  const previous = (await listLocalActionRetryEntries())[0];
  await resolveLocalActionRetryEntry(previous);
  await observations.advance();
  await waitFor(() => expect(result.current.outboundPending).toBe(false));
  await act(async () => result.current.compose.submitMessage({ ...fields, text_body: "Changed" }));
  expect(result.current.compose.retryDialog.open).toBe(true);
  await act(async () => result.current.compose.confirmRetry());
  expect(posts).toHaveLength(2);
  expect(posts[1].idempotency_key).not.toBe(posts[0].idempotency_key);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({ request_id: 72 });
  expect(result.current.compose.compose.submissionUnknown?.messageID).toBe("<attempt-2@example.invalid>");
});

it("protects server-only outbound uncertainty after remount until exact reconciliation", async () => {
  const posts: unknown[] = [];
  vi.stubGlobal("fetch", async (url: string, options?: RequestInit) => {
    if (options?.method === "POST") {
      posts.push(JSON.parse(String(options.body)));
      return json(connectorActionFixture({ target_ref: target.ref, action_name: "send_message" }));
    }
    const action = new URL(url).searchParams.get("action_name");
    return json(action === "send_message" ? [remote] : action ? [] : {});
  });
  await apiGet("/api/status");
  const render = () => renderHook(() => useMailWorkspace({ target, approvals: { state: "ready", data: [] } }));
  let hook = render();
  await waitFor(() => expect(observations.pending).toBe(1));
  expect(await listLocalActionRetryEntries()).toEqual([]);
  await act(async () => hook.result.current.runner.runMailAction("send_message", fields, "server-only uncertainty"));
  expect(posts).toHaveLength(0);
  hook.unmount();
  hook = render();
  await waitFor(() => expect(observations.pending).toBe(1));
  expect(hook.result.current.outboundPending).toBe(true);
  await reconcileVerifiedServerRequest({ key: mutationTestWorkspace }, remote);
  await observations.advance();
  await waitFor(() => expect(hook.result.current.outboundPending).toBe(false));
  act(() => hook.result.current.openCompose());
  await act(async () => hook.result.current.compose.submitMessage(fields));
  expect(posts).toHaveLength(1);
});

it.each([new APIError("invalid recipient", { status: 400 }), new APIError("storage unavailable", { code: "local_mutation_not_sent" })])(
  "does not label a definitive or pre-dispatch failure as unknown: %s",
  async (error) => {
    const run = vi.fn().mockRejectedValue(error);
    const { result } = renderHook(() =>
      useMailCompose({ scopeKey: "test", selectedMessage: null, outboundPending: false, runMailAction: run }),
    );
    act(() => result.current.openCompose());
    await act(async () => result.current.submitMessage(fields));
    expect(result.current.compose.submissionUnknown).toBeUndefined();
    act(() => result.current.closeCompose());
    act(() => result.current.openCompose());
    expect(result.current.compose.form).toEqual({});
  },
);

it("retains a generic unknown pending resolution and the draft across reopen paths", async () => {
  const run = vi
    .fn()
    .mockResolvedValue(connectorActionFixture({ request_id: 71, action_name: "reply_message", status: "approval_pending" }));
  const selectedMessage = {
    message_ref: { folder: "INBOX", uid: 1, uidvalidity: 1 },
    subject: "Report",
    from: [{ address: "synthetic@example.invalid" }],
  };
  const { result } = renderHook(() => useMailCompose({ scopeKey: "test", selectedMessage, outboundPending: false, runMailAction: run }));
  act(() => result.current.openReply());
  await act(async () => result.current.submitMessage(fields));
  await act(async () => result.current.submitMessage(fields));
  expect(run).toHaveBeenCalledTimes(1);
  act(() => result.current.closeCompose());
  expect(result.current.compose.pendingRequestID).toBe(71);
  act(() =>
    result.current.resolvePending(
      { requestID: 71, actionName: "reply_message", context: run.mock.calls[0][4], generation: 1, scope: "test" },
      { state: "failed", item: { action_name: "reply_message", status: "outcome_unknown" } },
    ),
  );
  act(() => result.current.closeCompose());
  act(() => result.current.openReply());
  expect(result.current.compose.submissionUnknown).toBeDefined();
  expect(result.current.compose.form).toEqual(fields);
});

it.each([
  { error: new TypeError("reply lost"), unknown: true },
  { error: new APIError("gateway unavailable", { status: 503 }), unknown: true },
  { error: submissionFailure("outcome_unknown"), unknown: true },
  { error: submissionFailure("failed"), unknown: false },
  { error: new APIError("SMTP rejected", { status: 403 }), unknown: false },
  { error: new APIError("reply not proved", { status: 409, data: { status: "outcome_unknown" } }), unknown: true },
])("preserves only uncertain submissions across close and reopen: $error", async ({ error, unknown }) => {
  const run = vi.fn().mockRejectedValue(error);
  const { result } = renderHook(() =>
    useMailCompose({ scopeKey: "test", selectedMessage: null, outboundPending: false, runMailAction: run }),
  );
  act(() => result.current.openCompose());
  await act(async () => result.current.submitMessage(fields));
  expect(Boolean(result.current.compose.submissionUnknown)).toBe(unknown);
  expect(result.current.outcome?.error).toBe(error.message);
  act(() => result.current.closeCompose());
  act(() => result.current.openCompose());
  expect(result.current.compose.form).toEqual(unknown ? fields : {});
  const edited = { ...fields, subject: "Edited" };
  await act(async () => result.current.submitMessage(edited));
  expect(result.current.retryDialog.open).toBe(unknown);
  if (unknown) expect(result.current.retryDialog).toMatchObject({ draftChanged: true, fields: edited });
  expect(run).toHaveBeenCalledTimes(unknown ? 1 : 2);
});

it("discards late submission failures when the target scope changes", async () => {
  let rejectLate: ((_error: Error) => void) | undefined;
  const run = vi.fn(
    () =>
      new Promise<never>((_resolve, reject) => {
        rejectLate = reject;
      }),
  );
  const { result, rerender } = renderHook(
    (scopeKey) => useMailCompose({ scopeKey, selectedMessage: null, outboundPending: false, runMailAction: run }),
    { initialProps: "first" },
  );
  act(() => result.current.openCompose());
  let submission: Promise<void> | undefined;
  act(() => {
    submission = result.current.submitMessage(fields);
  });
  rerender("second");
  await act(async () => {
    if (!rejectLate) throw new Error("Submission was not dispatched");
    rejectLate(new TypeError("old target response lost"));
    await submission;
  });
  expect(result.current.compose).toEqual({ open: false, reply: false, form: {} });
  expect(result.current.outcome).toBeNull();
});

it("does not close a draft when dispatch is blocked without an action result", async () => {
  const run = vi.fn().mockResolvedValue(null);
  const { result } = renderHook(() =>
    useMailCompose({ scopeKey: "test", selectedMessage: null, outboundPending: false, runMailAction: run }),
  );
  act(() => result.current.openCompose());
  await act(async () => result.current.submitMessage(fields));
  expect(result.current.compose.open).toBe(true);
  expect(result.current.outcome).toBeNull();
  expect(result.current.retryDialog.open).toBe(false);
});

it("cannot open or submit a draft while another outbound request owns the target", async () => {
  const run = vi.fn().mockResolvedValue(connectorActionFixture({ action_name: "send_message" }));
  const selectedMessage = { message_ref: { folder: "INBOX", uid: 1, uidvalidity: 1 } };
  const { result } = renderHook(() => useMailCompose({ scopeKey: "test", selectedMessage, outboundPending: true, runMailAction: run }));
  act(() => result.current.openCompose());
  act(() => result.current.openReply());
  await act(async () => result.current.submitMessage(fields));
  expect(run).not.toHaveBeenCalled();
  expect(result.current.compose).toEqual({ open: false, reply: false, form: {} });
  expect(result.current.outcome).toBeNull();
});
