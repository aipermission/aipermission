import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiGet as realGet, apiPost as realPost } from "../../../lib/api";
import { setupMutationRetryStorage, mutationTestWorkspace } from "../../../test/connector-mutation-test-state";
import { BulkCommandDialog } from "./bulk-command-dialog";
import { SSHConnectorToolbarActionsTemplate } from "./console";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), currentWorkspaceBinding: () => mutationTestWorkspace }));
const apiGet = vi.mocked(realGet);
const apiPost = vi.mocked(realPost);
setupMutationRetryStorage();

const target = { id: 7, name: "Example host", username: "operator", host: "host.example", port: 22, connector_kind: "ssh" };

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
});
afterEach(() => vi.useRealTimers());

async function startObservationTest(onRefresh = vi.fn(), targets = [target]) {
  const user = userEvent.setup();
  const props = { open: true, targets, selectedTarget: target, onClose: vi.fn(), onRefresh };
  const view = render(<BulkCommandDialog {...props} />);
  if (targets.length > 1) await user.click(screen.getByRole("button", { name: "All" }));
  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf tracked");
  const confirmation = `RUN ON ${targets.length} TARGETS`;
  await user.type(screen.getByPlaceholderText(confirmation), confirmation);
  await waitFor(() => expect(screen.getByRole("button", { name: "Run selected" })).toBeEnabled());
  vi.useFakeTimers();
  await act(async () => screen.getByRole("button", { name: "Run selected" }).click());
  return { view, props };
}

function acknowledge(status: "running" | "completed" = "running") {
  return { parallelism: 3, items: [{ request_id: 41, target_id: 7, target_name: target.name, status, stdout: "previous output" }] };
}

it.each(["network", "invalid-status", "wrong-runtime"])(
  "keeps execution state after %s read failure and recovers without POST",
  async (failure) => {
    apiPost.mockResolvedValue(acknowledge());
    if (failure === "network") apiGet.mockRejectedValueOnce(new Error("Transient read failure"));
    else apiGet.mockResolvedValueOnce({ id: 41, runtime_id: failure === "wrong-runtime" ? 8 : 7, status: "future" });
    apiGet.mockResolvedValue({ id: 41, runtime_id: 7, status: "completed", exit_code: 0, stdout: "verified output" });
    await startObservationTest();
    await act(async () => vi.advanceTimersByTimeAsync(1000));
    expect(screen.getByText("0/1 finished, 1 running")).toBeVisible();
    expect(screen.getByText(/Result refresh:/)).toBeVisible();
    await act(async () => screen.getByRole("button", { name: /Example host running #41/ }).click());
    expect(screen.getByText("previous output")).toBeVisible();
    expect(screen.getByRole("button", { name: "Run selected" })).toBeDisabled();
    await act(async () => vi.advanceTimersByTimeAsync(1500));
    expect(screen.getByText("1/1 finished")).toBeVisible();
    expect(screen.getByText("verified output")).toBeVisible();
    expect(screen.queryByText(/Result refresh:/)).not.toBeInTheDocument();
    expect(apiPost).toHaveBeenCalledOnce();
  },
);

it("does not overlap slow observations, aborts them on close and ignores late results after reopen", async () => {
  apiPost.mockResolvedValue(acknowledge());
  const pending = deferred();
  apiGet.mockReturnValue(pending.promise);
  const { view, props } = await startObservationTest();
  await act(async () => vi.advanceTimersByTimeAsync(8000));
  expect(apiGet).toHaveBeenCalledOnce();
  const signal = apiGet.mock.calls[0][1]?.signal;
  view.rerender(<BulkCommandDialog {...props} open={false} />);
  expect(signal?.aborted).toBe(true);
  view.rerender(<BulkCommandDialog {...props} />);
  await act(async () => pending.resolve({ id: 41, runtime_id: 7, status: "completed", stdout: "stale result" }));
  expect(screen.queryByText("1/1 finished")).not.toBeInTheDocument();
  expect(screen.queryByText("stale result")).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Command" })).toHaveValue("");
  view.unmount();
  await act(async () => vi.advanceTimersByTimeAsync(10000));
  expect(apiGet).toHaveBeenCalledOnce();
  expect(apiPost).toHaveBeenCalledOnce();
});

it("verifies terminal acknowledgements before another submission and keeps activity failures separate", async () => {
  apiPost.mockResolvedValue(acknowledge("completed"));
  apiGet.mockResolvedValue({ id: 41, runtime_id: 7, status: "completed", exit_code: 0 });
  await startObservationTest(vi.fn().mockRejectedValue(new Error("Activity unavailable")));
  expect(screen.getByText("1/1 finished")).toBeVisible();
  expect(screen.getByRole("button", { name: "Run selected" })).toBeDisabled();
  await act(async () => vi.advanceTimersByTimeAsync(1000));
  expect(apiGet).toHaveBeenCalledOnce();
  expect(screen.getByText("1/1 finished")).toBeVisible();
  expect(screen.getByText(/Activity unavailable/)).toBeVisible();
  expect(screen.queryByText(/1 failed/)).not.toBeInTheDocument();
  expect(apiPost).toHaveBeenCalledOnce();
});

it("does not let the original delayed acknowledgement refresh roll back a manually verified result", async () => {
  apiPost.mockResolvedValue(acknowledge());
  apiGet.mockResolvedValueOnce({ id: 41, runtime_id: 7, status: "completed", exit_code: 0, stdout: "verified output" });
  apiGet.mockRejectedValue(new Error("Late read failure"));
  await startObservationTest();
  await act(async () => screen.getByRole("button", { name: "Refresh" }).click());
  await act(async () => screen.getByRole("button", { name: /Example host completed #41/ }).click());
  expect(screen.getByText("verified output")).toBeVisible();
  await act(async () => vi.advanceTimersByTimeAsync(3000));
  expect(screen.getByText("1/1 finished")).toBeVisible();
  expect(screen.getByText("verified output")).toBeVisible();
  expect(screen.queryByText("previous output")).not.toBeInTheDocument();
  expect(screen.queryByText(/Result refresh:/)).not.toBeInTheDocument();
  expect(apiGet).toHaveBeenCalledOnce();
  expect(apiPost).toHaveBeenCalledOnce();
});

it("ignores a stale acknowledgement activity failure after a newer activity refresh succeeds", async () => {
  const activity = deferred();
  const onRefresh = vi.fn().mockReturnValueOnce(activity.promise).mockResolvedValue(undefined);
  apiPost.mockResolvedValue(acknowledge());
  apiGet.mockResolvedValue({ id: 41, runtime_id: 7, status: "completed", exit_code: 0 });
  await startObservationTest(onRefresh);
  expect(onRefresh).toHaveBeenCalledOnce();
  await act(async () => screen.getByRole("button", { name: "Refresh" }).click());
  expect(onRefresh).toHaveBeenCalledTimes(2);
  expect(screen.getByText("1/1 finished")).toBeVisible();
  await act(async () => activity.reject(new Error("Stale activity failure")));
  expect(screen.queryByText(/Stale activity failure/)).not.toBeInTheDocument();
  expect(screen.getByText("1/1 finished")).toBeVisible();
  expect(apiPost).toHaveBeenCalledOnce();
});

it("preserves a completed sibling when another result read fails", async () => {
  const other = { ...target, id: 8, name: "Other host" };
  apiPost.mockResolvedValue({
    parallelism: 3,
    items: [acknowledge().items[0], { request_id: 42, target_id: 8, target_name: other.name, status: "running" }],
  });
  apiGet.mockImplementation(async (path) => {
    if (path.endsWith("/42")) throw new Error("Second result unavailable");
    return { id: 41, runtime_id: 7, status: "completed", exit_code: 0 };
  });
  await startObservationTest(vi.fn(), [target, other]);
  await act(async () => vi.advanceTimersByTimeAsync(1000));
  expect(screen.getByText("1/2 finished, 1 running")).toBeVisible();
  expect(screen.getByText(/Second result unavailable/)).toBeVisible();
  await act(async () => vi.advanceTimersByTimeAsync(2500));
  expect(apiGet.mock.calls.filter(([path]) => path.endsWith("/41"))).toHaveLength(1);
  expect(apiGet.mock.calls.filter(([path]) => path.endsWith("/42"))).toHaveLength(2);
  expect(apiPost).toHaveBeenCalledOnce();
});

it("preserves real bulk dialog drafts through toolbar projections and resets them only for a new runtime", async () => {
  const user = userEvent.setup();
  const props = {
    theme: "dark" as const,
    selectedRuntimeTarget: target,
    liveConsoleTargets: [target],
    selectedSession: null,
    selectedSessionLive: false,
  };
  const view = render(<SSHConnectorToolbarActionsTemplate {...props} />);
  await user.click(screen.getByRole("button", { name: "Bulk" }));
  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf unchanged");
  await user.type(screen.getByRole("textbox", { name: "Reason" }), "Keep this draft");
  view.rerender(
    <SSHConnectorToolbarActionsTemplate {...props} selectedRuntimeTarget={{ ...target }} liveConsoleTargets={[{ ...target }]} />,
  );
  expect(screen.getByRole("textbox", { name: "Command" })).toHaveValue("printf unchanged");
  expect(screen.getByRole("textbox", { name: "Reason" })).toHaveValue("Keep this draft");
  const next = { ...target, id: 8, name: "Other host" };
  view.rerender(<SSHConnectorToolbarActionsTemplate {...props} selectedRuntimeTarget={next} liveConsoleTargets={[next]} />);
  expect(screen.getByRole("textbox", { name: "Command" })).toHaveValue("");
  expect(screen.getByRole("textbox", { name: "Reason" })).toHaveValue("");
  expect(screen.getByRole("checkbox", { name: "Select Other host" })).toBeChecked();
  expect(apiPost).not.toHaveBeenCalled();
});

it("submits the selected targets with the exact bulk command contract", async () => {
  const user = userEvent.setup();
  const onRefresh = vi.fn();
  apiPost.mockResolvedValue({
    parallelism: 3,
    items: [{ request_id: 41, target_id: 7, target_name: "Example host", status: "completed", exit_code: 0, stdout: "ok" }],
  });
  render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={onRefresh} />);

  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf ok");
  await user.type(screen.getByRole("textbox", { name: "Reason" }), "Regression test");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));

  await waitFor(() => expect(onRefresh).toHaveBeenCalledOnce());
  expect(apiPost).toHaveBeenCalledWith(
    "/api/console/bulk-exec",
    {
      target_ids: [7],
      command: "printf ok",
      reason: "Regression test",
      confirmation: "RUN ON 1 TARGETS",
    },
    { signal: expect.any(AbortSignal), exclusiveConsoleBatch: true },
  );
  expect(await screen.findByText("1/1 finished")).toBeVisible();
});

it("manually refreshes existing bulk results without forwarding the click event", async () => {
  const user = userEvent.setup();
  const onRefresh = vi.fn();
  apiPost.mockResolvedValue({
    parallelism: 3,
    items: [{ request_id: 41, target_id: 7, target_name: "Example host", status: "completed", exit_code: 0, stdout: "old" }],
  });
  apiGet.mockResolvedValue({ id: 41, runtime_id: 7, status: "completed", exit_code: 0, stdout: "fresh" });
  render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={onRefresh} />);

  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf ok");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));
  await screen.findByText("1/1 finished");
  await user.click(screen.getByRole("button", { name: "Refresh" }));

  await waitFor(() =>
    expect(apiGet).toHaveBeenCalledWith("/api/console/command-requests/41", {
      signal: expect.any(AbortSignal),
      workspaceBinding: mutationTestWorkspace,
      timeoutMs: 10000,
    }),
  );
  await waitFor(() => expect(onRefresh).toHaveBeenCalledTimes(2));
  expect(screen.queryByText(/items\.map|is not a function/i)).not.toBeInTheDocument();
});

it("does not restore an old run after the dialog closes and reopens", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValue(pending.promise);
  const view = render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);

  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf old");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));
  const options: unknown = apiPost.mock.calls[0][2];
  if (!options || typeof options !== "object" || !("signal" in options)) throw new Error("Missing request options");
  const signal = options.signal;
  if (!(signal instanceof AbortSignal)) throw new Error("Missing request signal");
  view.rerender(<BulkCommandDialog open={false} targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);
  view.rerender(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);
  expect(signal.aborted).toBe(true);
  pending.resolve({ items: [{ request_id: 41, target_name: "Example host", status: "completed" }] });

  await waitFor(() => expect(screen.queryByText("1/1 finished")).not.toBeInTheDocument());
  expect(screen.getByRole("textbox", { name: "Command" })).toHaveValue("");
});

it("stops automatic polling for uncertain command outcomes without allowing another mutation", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({
    parallelism: 3,
    items: [{ request_id: 42, target_id: 7, target_name: "Example host", status: "outcome_unknown", error: "Inspect before retrying" }],
  });
  render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);

  await user.type(screen.getByRole("textbox", { name: "Command" }), "deploy");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));

  expect(await screen.findByText("1/1 finished")).toBeVisible();
  expect(screen.getByRole("button", { name: "Run selected" })).toBeDisabled();
});

it("copies the exact confirmation phrase", async () => {
  const user = userEvent.setup();
  const writeText = vi.fn();
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);

  await user.click(screen.getByTitle("Copy confirmation phrase"));

  expect(writeText).toHaveBeenCalledWith("RUN ON 1 TARGETS");
});

it("searches target metadata and invalidates confirmation after every selection change", async () => {
  const user = userEvent.setup();
  const other = { ...target, id: 8, name: "Other host", host: "other.example" };
  render(<BulkCommandDialog open targets={[target, other]} selectedTarget={target} onClose={vi.fn()} />);
  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf ok");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  expect(screen.getByRole("button", { name: "Run selected" })).toBeEnabled();
  await user.click(screen.getByRole("checkbox", { name: "Select Other host" }));
  expect(screen.getByPlaceholderText("RUN ON 2 TARGETS")).toHaveValue("");
  expect(screen.getByRole("button", { name: "Run selected" })).toBeDisabled();
  await user.type(screen.getByPlaceholderText("RUN ON 2 TARGETS"), "RUN ON 2 TARGETS");
  expect(screen.getByRole("button", { name: "Run selected" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "None" }));
  expect(screen.getByPlaceholderText("RUN ON 0 TARGETS")).toHaveValue("");
  expect(screen.getByRole("checkbox", { name: "Select Example host" })).not.toBeChecked();
  await user.type(screen.getByPlaceholderText("RUN ON 0 TARGETS"), "RUN ON 0 TARGETS");
  await user.click(screen.getByRole("button", { name: "All" }));
  expect(screen.getByPlaceholderText("RUN ON 2 TARGETS")).toHaveValue("");
  expect(screen.getByRole("checkbox", { name: "Select Other host" })).toBeChecked();
  await user.type(screen.getByPlaceholderText("Search targets"), "OTHER.EXAMPLE");
  expect(screen.queryByRole("checkbox", { name: "Select Example host" })).not.toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: "Select Other host" })).toBeChecked();
  await user.clear(screen.getByPlaceholderText("Search targets"));
  await user.type(screen.getByPlaceholderText("Search targets"), "unknown");
  expect(screen.getByText("No matching targets.")).toBeVisible();
  expect(apiPost).not.toHaveBeenCalled();
});

it("toggles compact result details and exposes captured failure output without rerunning a command", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({
    parallelism: 3,
    items: [{ request_id: 41, target_id: 7, target_name: target.name, status: "failed", exit_code: 1, stderr: "Permission denied" }],
  });
  render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} />);
  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf ok");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));
  expect(await screen.findByText("1/1 finished, 1 failed")).toBeVisible();
  expect(screen.queryByText("Permission denied")).not.toBeInTheDocument();
  const row = screen.getByRole("button", { name: /Example host failed #41 exit 1/ });
  await user.click(row);
  expect(screen.getByText("Permission denied")).toBeVisible();
  expect(screen.getByText("Request #41")).toBeVisible();
  await user.click(row);
  expect(screen.queryByText("Permission denied")).not.toBeInTheDocument();
  expect(screen.getByText("Select a result on the left to inspect its captured console output.")).toBeVisible();
  expect(apiPost).toHaveBeenCalledOnce();
});
