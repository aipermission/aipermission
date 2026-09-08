import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { expect, it, vi } from "vitest";
import { Drawer } from "./drawer";

it("traps focus and closes with Escape", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  render(
    <Drawer open title="Transfers" onClose={onClose}>
      <button type="button">First action</button>
      <button type="button">Last action</button>
    </Drawer>,
  );

  await waitFor(() => expect(screen.getByRole("button", { name: "Close drawer" })).toHaveFocus());
  await user.tab({ shift: true });
  expect(screen.getByRole("button", { name: "Last action" })).toHaveFocus();
  fireEvent.keyDown(document.activeElement, { key: "Escape" });
  expect(onClose).toHaveBeenCalledOnce();
});

it("closes from its labelled close control", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  render(
    <Drawer open title="Messages" description="Runtime messages" onClose={onClose}>
      Body
    </Drawer>,
  );
  await user.click(screen.getByRole("button", { name: "Close drawer" }));
  expect(onClose).toHaveBeenCalledOnce();
});

it("restores focus to the opener after closing", async () => {
  const user = userEvent.setup();
  function Harness() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button type="button" onClick={() => setOpen(true)}>
          Open drawer
        </button>
        <Drawer open={open} title="Messages" onClose={() => setOpen(false)}>
          Body
        </Drawer>
      </>
    );
  }
  render(<Harness />);
  const opener = screen.getByRole("button", { name: "Open drawer" });
  await user.click(opener);
  await user.keyboard("{Escape}");
  await waitFor(() => expect(opener).toHaveFocus());
});
