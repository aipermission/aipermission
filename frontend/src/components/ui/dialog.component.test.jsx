import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Dialog } from "./dialog";

function DialogHarness({ closeDisabled = false, closeOnOverlay = true, closeOnEscape = true, onClose = () => {} }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open settings
      </button>
      <Dialog
        open={open}
        title="Connection settings"
        onClose={() => {
          onClose();
          setOpen(false);
        }}
        closeDisabled={closeDisabled}
        closeOnOverlay={closeOnOverlay}
        closeOnEscape={closeOnEscape}
      >
        <button type="button">Save</button>
      </Dialog>
    </>
  );
}

describe("Dialog", () => {
  it("moves focus into the dialog and restores the opener after Escape", async () => {
    const user = userEvent.setup();
    render(<DialogHarness />);
    const opener = screen.getByRole("button", { name: "Open settings" });

    await user.click(opener);
    const dialog = screen.getByRole("dialog", { name: "Connection settings" });
    expect(within(dialog).getByRole("button", { name: "Close dialog" })).toHaveFocus();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(opener).toHaveFocus();
  });

  it("supports overlay dismissal when enabled", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<DialogHarness onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Open settings" }));
    await user.click(screen.getByRole("button", { name: "Dismiss dialog" }));

    expect(onClose).toHaveBeenCalledOnce();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("blocks Escape and overlay dismissal while close is disabled", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<DialogHarness closeDisabled onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Open settings" }));
    await user.keyboard("{Escape}");

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Dismiss dialog" })).not.toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});
