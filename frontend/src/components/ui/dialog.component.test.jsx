import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Dialog } from "./dialog";

function DialogHarness({ closeDisabled = false, closeOnOverlay = true, closeOnEscape = true, onClose = () => {}, size }) {
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
        size={size}
      >
        <button type="button">Save</button>
      </Dialog>
    </>
  );
}

function StackedDialogHarness({ onParentClose = () => {}, onChildClose = () => {} }) {
  const [parentOpen, setParentOpen] = useState(false);
  const [childOpen, setChildOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setParentOpen(true)}>
        Open parent
      </button>
      <Dialog
        open={parentOpen}
        title="Parent dialog"
        onClose={() => {
          onParentClose();
          setParentOpen(false);
        }}
      >
        <button type="button" onClick={() => setChildOpen(true)}>
          Open child
        </button>
        <Dialog
          open={childOpen}
          title="Child dialog"
          onClose={() => {
            onChildClose();
            setChildOpen(false);
          }}
        >
          <button type="button">Child action</button>
        </Dialog>
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
    await waitFor(() => expect(opener).toHaveFocus());
  });

  it("contains forward and backward keyboard focus inside the active dialog", async () => {
    const user = userEvent.setup();
    render(<DialogHarness />);

    await user.click(screen.getByRole("button", { name: "Open settings" }));
    const dialog = screen.getByRole("dialog", { name: "Connection settings" });
    const close = within(dialog).getByRole("button", { name: "Close dialog" });
    const save = within(dialog).getByRole("button", { name: "Save" });
    expect(close).toHaveFocus();

    await user.tab();
    expect(save).toHaveFocus();
    await user.tab();
    expect(close).toHaveFocus();
    await user.tab({ shift: true });
    expect(save).toHaveFocus();
  });

  it("hides background content from assistive interaction while open", async () => {
    const user = userEvent.setup();
    render(<DialogHarness />);
    const opener = screen.getByRole("button", { name: "Open settings" });

    await user.click(opener);
    expect(opener.closest('[aria-hidden="true"]')).not.toBeNull();
    await user.keyboard("{Escape}");
    expect(opener.closest('[aria-hidden="true"]')).toBeNull();
  });

  it("supports overlay dismissal when enabled", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<DialogHarness onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Open settings" }));
    await user.click(screen.getByTestId("dialog-overlay"));

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
    expect(screen.getByTestId("dialog-overlay")).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("honors independent Escape and overlay dismissal policies", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<DialogHarness closeOnOverlay={false} closeOnEscape={false} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Open settings" }));
    await user.click(screen.getByTestId("dialog-overlay"));
    await user.keyboard("{Escape}");

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("keeps Escape ownership with the topmost stacked dialog", async () => {
    const user = userEvent.setup();
    const onParentClose = vi.fn();
    const onChildClose = vi.fn();
    render(<StackedDialogHarness onParentClose={onParentClose} onChildClose={onChildClose} />);

    await user.click(screen.getByRole("button", { name: "Open parent" }));
    await user.click(screen.getByRole("button", { name: "Open child" }));
    const dialogs = document.querySelectorAll('[role="dialog"]');
    expect(dialogs).toHaveLength(2);
    expect(dialogs[0].getAttribute("aria-labelledby")).not.toBe(dialogs[1].getAttribute("aria-labelledby"));

    await user.keyboard("{Escape}");
    expect(onChildClose).toHaveBeenCalledOnce();
    expect(onParentClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Parent dialog" })).toBeInTheDocument();

    await user.keyboard("{Escape}");
    expect(onParentClose).toHaveBeenCalledOnce();
  });

  it("falls back to the small frame for an unknown size", async () => {
    const user = userEvent.setup();
    render(<DialogHarness size="unknown" />);
    await user.click(screen.getByRole("button", { name: "Open settings" }));
    expect(screen.getByRole("dialog", { name: "Connection settings" })).toHaveClass("max-w-md");
  });
});
