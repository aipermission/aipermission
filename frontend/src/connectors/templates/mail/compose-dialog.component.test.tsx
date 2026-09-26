import { fireEvent, render, screen } from "@testing-library/react";
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
  expect(screen.getByRole("button", { name: "Formatted" })).toHaveClass("bg-emerald-950");
  expect(editor).toHaveAttribute("contenteditable", "true");
  expect(editor).toHaveAttribute("aria-multiline", "true");
  expect(editor).toHaveAccessibleDescription(/gateway sanitizes formatted content/i);
  expect(screen.getByRole("toolbar", { name: "Message formatting" })).toContainElement(screen.getByRole("button", { name: "Bold" }));
  expect(screen.getByRole("button", { name: "Bold" }).closest("label")).toBeNull();
});

it("normalizes stored recipient arrays and submits only a validated complete draft", () => {
  const onSubmit = vi.fn();
  render(
    <ComposeDialog
      draft={{
        open: true,
        reply: false,
        form: { to: ["one@example.test", "two@example.test"], cc: [], bcc: [], subject: "Status", text_body: "Ready" },
      }}
      busy={false}
      error=""
      onClose={vi.fn()}
      onSubmit={onSubmit}
    />,
  );
  expect(screen.getByRole("textbox", { name: "To" })).toHaveValue("one@example.test, two@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
  expect(onSubmit).toHaveBeenCalledWith({
    to: ["one@example.test", "two@example.test"],
    cc: [],
    bcc: [],
    subject: "Status",
    text_body: "Ready",
    html_body: "",
  });
  fireEvent.change(screen.getByRole("textbox", { name: "Subject" }), { target: { value: "x".repeat(513) } });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
  expect(screen.getByText(/512 bytes/)).toBeInTheDocument();
  expect(onSubmit).toHaveBeenCalledTimes(1);
});

it("keeps formatted content and its plain fallback consistent across mode changes", async () => {
  const onSubmit = vi.fn();
  render(
    <ComposeDialog
      draft={{ open: true, reply: true, form: { to: "one@example.test", subject: "Status", text_body: "Ready\nNow" } }}
      busy={false}
      error=""
      onClose={vi.fn()}
      onSubmit={onSubmit}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Formatted" }));
  const editor = screen.getByRole("textbox", { name: "Message" });
  expect(editor.innerHTML).toBe("Ready<br>Now");
  editor.innerHTML = "<strong>Updated</strong>";
  fireEvent.input(editor);
  expect(screen.getByRole("textbox", { name: /Plain-text fallback/ })).toHaveValue("Updated");
  await userEvent.click(screen.getByRole("button", { name: "Plain text" }));
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue("Updated");
  await userEvent.click(screen.getByRole("button", { name: "Formatted" }));
  expect(screen.getByRole("textbox", { name: "Message" }).innerHTML).toBe("<strong>Updated</strong>");
  await userEvent.click(screen.getByRole("button", { name: "Send reply" }));
  expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ text_body: "Updated", html_body: "<strong>Updated</strong>" }));
});

it("pastes only plain text and prevents dropped external markup", async () => {
  render(
    <ComposeDialog
      draft={{ open: true, reply: false, form: { to: "one@example.test", subject: "Status", text_body: "Ready" } }}
      busy={false}
      error=""
      onClose={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Formatted" }));
  const editor = screen.getByRole("textbox", { name: "Message" });
  const selection = window.getSelection();
  if (!selection) throw new Error("Selection is unavailable.");
  const range = document.createRange();
  range.selectNodeContents(editor);
  selection.removeAllRanges();
  selection.addRange(range);
  const getData = vi.fn(() => "<script>alert(1)</script>\nPlain text");
  fireEvent.paste(editor, { clipboardData: { getData } });
  expect(getData).toHaveBeenCalledWith("text/plain");
  expect(editor.querySelector("script")).toBeNull();
  expect(editor.textContent).toContain("<script>alert(1)</script>");
  expect(editor.querySelector("br")).not.toBeNull();
  expect(fireEvent.drop(editor)).toBe(false);
});

it("restores the saved link selection and handles Enter without submitting the draft", async () => {
  const onSubmit = vi.fn();
  const original = Object.getOwnPropertyDescriptor(document, "execCommand");
  const command = vi.fn();
  Object.defineProperty(document, "execCommand", { configurable: true, value: command });
  try {
    render(
      <ComposeDialog
        draft={{ open: true, reply: false, form: { to: "one@example.test", subject: "Status", text_body: "Ready" } }}
        busy={false}
        error=""
        onClose={vi.fn()}
        onSubmit={onSubmit}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Formatted" }));
    const editor = screen.getByRole("textbox", { name: "Message" });
    editor.focus();
    const selection = window.getSelection();
    if (!selection) throw new Error("Selection is unavailable.");
    const range = document.createRange();
    range.selectNodeContents(editor);
    selection.removeAllRanges();
    selection.addRange(range);
    fireEvent.click(screen.getByRole("button", { name: "Link" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Link URL" }), { target: { value: "javascript:alert(1)" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply link" }));
    expect(command).not.toHaveBeenCalled();
    expect(screen.getByText(/http, https, or mailto/)).toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "Link URL" }), { target: { value: "https://example.test/docs" } });
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Link URL" }), { key: "Enter" });
    expect(command).toHaveBeenCalledWith("createLink", false, "https://example.test/docs");
    expect(selection.toString()).toBe("Ready");
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.queryByRole("textbox", { name: "Link URL" })).not.toBeInTheDocument();
  } finally {
    if (original) Object.defineProperty(document, "execCommand", original);
    else Reflect.deleteProperty(document, "execCommand");
  }
});
