import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet as realGet, apiPost as realPost } from "../../../lib/api";
import { BulkCommandDialog } from "./bulk-command-dialog";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));
const apiGet = vi.mocked(realGet);
const apiPost = vi.mocked(realPost);

const target = { id: 7, name: "Example host", username: "operator", host: "host.example", port: 22, connector_kind: "ssh" };

function deferred() {
  let resolve!: (_value: unknown) => void;
  const promise = new Promise<unknown>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
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
    { signal: expect.any(AbortSignal) },
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
  apiGet.mockResolvedValue({ id: 41, status: "completed", exit_code: 0, stdout: "fresh" });
  render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={onRefresh} />);

  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf ok");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));
  await screen.findByText("1/1 finished");
  await user.click(screen.getByRole("button", { name: "Refresh" }));

  await waitFor(() => expect(apiGet).toHaveBeenCalledWith("/api/console/command-requests/41", { signal: expect.any(AbortSignal) }));
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

it("treats uncertain command outcomes as terminal", async () => {
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
