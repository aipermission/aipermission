import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../../lib/api";
import { BulkCommandDialog } from "./bulk-command-dialog";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

const target = { id: 7, name: "Example host", username: "operator", host: "host.example", port: 22, connector_kind: "ssh" };

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
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
    items: [{ request_id: 41, target_name: "Example host", status: "completed", exit_code: 0, stdout: "ok" }],
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

it("does not restore an old run after the dialog closes and reopens", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValue(pending.promise);
  const view = render(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);

  await user.type(screen.getByRole("textbox", { name: "Command" }), "printf old");
  await user.type(screen.getByPlaceholderText("RUN ON 1 TARGETS"), "RUN ON 1 TARGETS");
  await user.click(screen.getByRole("button", { name: "Run selected" }));
  const signal = apiPost.mock.calls[0][2].signal;
  view.rerender(<BulkCommandDialog open={false} targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);
  view.rerender(<BulkCommandDialog open targets={[target]} selectedTarget={target} onClose={vi.fn()} onRefresh={vi.fn()} />);
  expect(signal.aborted).toBe(true);
  pending.resolve({ items: [{ request_id: 41, target_name: "Example host", status: "completed" }] });

  await waitFor(() => expect(screen.queryByText("1/1 finished")).not.toBeInTheDocument());
  expect(screen.getByRole("textbox", { name: "Command" })).toHaveValue("");
});
