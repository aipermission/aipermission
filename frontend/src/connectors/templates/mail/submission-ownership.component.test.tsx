import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useMailWorkspace } from "./use-mail-workspace";
import { listLocalActionRetryEntries, resolveLocalActionRetryEntry } from "../../../lib/local-action-retry";
import { mutationTestWorkspace, setupMutationRetryStorage } from "../../../test/connector-mutation-test-state";

setupMutationRetryStorage();
vi.mock("../_shared/use-connector-mutation-ownership", () => ({
  useConnectorMutationOwnership: () => ({
    locked: false,
    run: (operation: (_onPending: () => void) => Promise<unknown>) => operation(() => {}),
  }),
}));

it.each(["lost-reply", "gateway-outcome-unknown"])("protects a changed draft after %s until explicit reconciliation", async (mode) => {
  const sent: Array<{
    idempotency_key: string;
    input: Record<string, unknown>;
  }> = [];
  const fetcher = vi.fn(async (_url: string, options: RequestInit) => {
    const body = JSON.parse(String(options.body));
    sent.push(body);
    if (sent.length === 1 && mode === "lost-reply") throw new TypeError("response lost after dispatch");
    return new Response(
      JSON.stringify({
        request_id: sent.length,
        target_ref: "mail:1:1",
        connector_kind: "mail",
        action_name: "send_message",
        status: sent.length === 1 ? "outcome_unknown" : "completed",
        ...(sent.length === 1
          ? {
              error: "Gateway restarted before it could prove the remote outcome.",
            }
          : {}),
        retry_policy: {
          class: "non_idempotent",
          guidance: "Reconcile original submission before another external attempt.",
        },
      }),
      {
        status: 200,
        headers: { "X-AIPermission-Workspace": mutationTestWorkspace },
      },
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { result } = renderHook(() =>
    useMailWorkspace({
      target: {
        ref: "mail:1:1",
        public: { imap_enabled: false, smtp_auth_mode: "reuse_imap" },
      },
      approvals: { data: [] },
      session: { active: true, startedAt: "review-session" },
    }),
  );
  const first = {
    to: ["synthetic@example.invalid"],
    cc: [],
    bcc: [],
    subject: "Deployment report",
    text_body: "Version one",
    html_body: "",
  };
  act(() => result.current.compose.openCompose());
  await act(async () => result.current.compose.submitMessage(first));
  expect(sent).toHaveLength(1);
  const firstEntry = (await listLocalActionRetryEntries())[0];
  expect(firstEntry).toMatchObject({
    action_name: "send_message",
    state: mode === "lost-reply" ? "pending" : "outcome_unknown",
  });
  expect(result.current.compose.compose.submissionUnknown).toBeDefined();
  await act(async () =>
    result.current.compose.submitMessage({
      ...first,
      text_body: "Version one (typo fixed)",
    }),
  );
  expect(result.current.compose.retryDialog.open).toBe(true);
  await act(async () => result.current.compose.confirmRetry());
  expect(sent).toHaveLength(1);
  act(() => result.current.compose.closeCompose());
  act(() => result.current.compose.openCompose());
  expect(result.current.compose.compose.submissionUnknown).toBeDefined();
  expect((await listLocalActionRetryEntries())[0].key).toBe(firstEntry.key);
  await resolveLocalActionRetryEntry((await listLocalActionRetryEntries())[0]);
  await act(async () => result.current.compose.confirmRetry());
  expect(sent).toHaveLength(2);
  expect(sent[1].idempotency_key).not.toBe(firstEntry.key);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});
