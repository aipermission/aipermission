import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ClearDownloadDialog, OverwriteConfirmDialog, UnsavedDownloadCloseDialog } from "./file-transfer-confirm-dialogs";

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
    expect(screen.queryByRole("button", { name: "Dismiss dialog" })).not.toBeInTheDocument();
    expect(onCancel).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Overwrite all" }));
    expect(onOverwrite).toHaveBeenCalledOnce();
  });

  it("keeps unsaved download clear choices explicit", async () => {
    const user = userEvent.setup();
    const handlers = { onCancel: vi.fn(), onContinue: vi.fn(), onSave: vi.fn() };
    render(<ClearDownloadDialog open {...handlers} />);

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Clear anyway" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(handlers.onCancel).toHaveBeenCalledOnce();
    expect(handlers.onContinue).toHaveBeenCalledOnce();
    expect(handlers.onSave).toHaveBeenCalledOnce();
  });

  it("keeps unsaved download close choices explicit", async () => {
    const user = userEvent.setup();
    const handlers = { onCancel: vi.fn(), onCloseAnyway: vi.fn(), onSave: vi.fn() };
    render(<UnsavedDownloadCloseDialog open {...handlers} />);

    await user.click(screen.getByRole("button", { name: "Close anyway" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(handlers.onCloseAnyway).toHaveBeenCalledOnce();
    expect(handlers.onSave).toHaveBeenCalledOnce();
  });
});
