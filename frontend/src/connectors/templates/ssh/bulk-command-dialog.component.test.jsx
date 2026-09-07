import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../../lib/api";
import { BulkCommandDialog } from "./bulk-command-dialog";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

const target = { id: 7, name: "Example host", username: "operator", host: "host.example", port: 22 };

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
  expect(apiPost).toHaveBeenCalledWith("/api/console/bulk-exec", {
    target_ids: [7],
    command: "printf ok",
    reason: "Regression test",
    confirmation: "RUN ON 1 TARGETS",
  });
  expect(await screen.findByText("1/1 finished")).toBeVisible();
});
