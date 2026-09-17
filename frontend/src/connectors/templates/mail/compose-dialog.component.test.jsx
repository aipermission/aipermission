import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ComposeDialog } from "./compose-dialog";

it("gives the formatted message editor explicit textbox and toolbar semantics", async () => {
  render(
    <ComposeDialog
      draft={{
        open: true,
        reply: false,
        form: {
          to: "operator@example.com",
          subject: "Status",
          text_body: "Current status",
          html_body: "",
        },
      }}
      busy={false}
      error=""
      onClose={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );

  await userEvent.click(screen.getByRole("button", { name: "Formatted" }));

  const editor = screen.getByRole("textbox", { name: "Message" });
  expect(editor).toHaveAttribute("contenteditable", "true");
  expect(editor).toHaveAttribute("aria-multiline", "true");
  expect(editor).toHaveAccessibleDescription(/gateway sanitizes formatted content/i);
  expect(screen.getByRole("toolbar", { name: "Message formatting" })).toContainElement(screen.getByRole("button", { name: "Bold" }));
  expect(screen.getByRole("button", { name: "Bold" }).closest("label")).toBeNull();
});
