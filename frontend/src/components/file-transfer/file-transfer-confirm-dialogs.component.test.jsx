import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { OverwriteConfirmDialog } from "./file-transfer-confirm-dialogs";

describe("OverwriteConfirmDialog", () => {
  it("keeps destructive confirmation explicit and lists every conflict", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();
    const onOverwrite = vi.fn();
    render(
      <OverwriteConfirmDialog
        open
        conflicts={[{ remote_path: "/home/report.csv" }, { remote_path: "/home/archive/report.csv" }]}
        onCancel={onCancel}
        onOverwrite={onOverwrite}
      />,
    );

    expect(screen.getByText("/home/report.csv")).toBeInTheDocument();
    expect(screen.getByText("/home/archive/report.csv")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "Dismiss dialog" }));
    expect(onCancel).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Overwrite all" }));
    expect(onOverwrite).toHaveBeenCalledOnce();
  });
});
