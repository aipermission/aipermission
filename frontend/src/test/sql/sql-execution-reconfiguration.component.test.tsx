import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";
import { useSQLConsole } from "../../connectors/templates/_shared/use-sql-console";
import { executionApproval, executionProps, executionResponse, setupSQLExecutionTransport } from "./execution-test-state";
import type { SQLConsoleProps } from "../../connectors/templates/_shared/use-sql-console";

const transport = setupSQLExecutionTransport();
const props: SQLConsoleProps = {
  ...executionProps,
  target: { ...executionProps.target, config: { host: "first.test", port: 1433, database: "First" } },
  executionIdentity: {
    config: { tls_mode: "verify_full" },
    project_id: 1,
    updated_at: "v1",
    profile_kind: "username_password",
    profile_label: "main",
    public: { username: "reader" },
  },
};

it.each(["endpoint", "project", "profile", "revision", "transport"])(
  "retires armed and retained handlers after same-reference %s changes",
  async (change) => {
    const { result, rerender } = renderHook((value: SQLConsoleProps) => useSQLConsole(value), { initialProps: props });
    await waitFor(() => expect(result.current.executionLocked).toBe(false));
    act(() => {
      result.current.setExecutionMode(true);
      result.current.setSQL("DELETE FROM items");
    });
    const staleSubmit = result.current.runQuery;
    const next = { ...props, executionIdentity: { ...props.executionIdentity! } };
    if (change === "endpoint") next.target = { ...props.target, config: { host: "second.test", database: "Second" } };
    if (change === "project") next.executionIdentity.project_id = 2;
    if (change === "profile") next.executionIdentity.public = { username: "writer" };
    if (change === "revision") next.executionIdentity.updated_at = "v2";
    if (change === "transport") next.executionIdentity.config = { tls_mode: "disable" };
    const metadataBefore = transport.posts.mock.calls.filter(([body]) => body.action_name === "get_metadata").length;
    rerender(next);
    expect(result.current.executionMode).toBe(false);
    await act(async () => {
      await staleSubmit();
      await result.current.runQuery();
    });
    expect(transport.execute).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(transport.posts.mock.calls.filter(([body]) => body.action_name === "get_metadata").length).toBe(metadataBefore + 1),
    );
  },
);

it("keeps equivalent polled metadata armed without duplicate catalog fetches", async () => {
  const { result, rerender } = renderHook((value: SQLConsoleProps) => useSQLConsole(value), { initialProps: props });
  await waitFor(() => expect(result.current.metadata.state).toBe("ready"));
  act(() => result.current.setExecutionMode(true));
  const before = transport.posts.mock.calls.length;
  rerender({
    ...props,
    target: { ...props.target, config: { database: "First", port: 1433, host: "first.test" } },
    executionIdentity: { ...props.executionIdentity! },
  });
  expect(result.current.executionMode).toBe(true);
  expect(transport.posts).toHaveBeenCalledTimes(before);
});

it("retains unresolved mutation ownership when the same target is reconfigured", async () => {
  transport.execute.mockResolvedValue(executionResponse({ status: "outcome_unknown", error: "lost observation" }));
  const { result, rerender } = renderHook((value: SQLConsoleProps) => useSQLConsole(value), { initialProps: props });
  await waitFor(() => expect(result.current.executionLocked).toBe(false));
  act(() => {
    result.current.setExecutionMode(true);
    result.current.setSQL("UPDATE items SET value=7");
  });
  await act(async () => result.current.runQuery());
  transport.discover.mockResolvedValue([executionApproval({ status: "outcome_unknown" })]);
  rerender({ ...props, target: { ...props.target, config: { host: "second.test", database: "Second" } } });
  expect(result.current.executionMode).toBe(false);
  expect(result.current.executionLocked).toBe(true);
  act(() => result.current.setExecutionMode(true));
  await act(async () => result.current.runQuery());
  expect(transport.execute).toHaveBeenCalledOnce();
});
