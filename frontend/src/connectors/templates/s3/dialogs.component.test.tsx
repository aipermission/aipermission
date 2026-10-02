import { fireEvent, render, screen } from "@testing-library/react";
import { useState, type FormEvent } from "react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { defaultS3ConfirmDialog, defaultUploadDialog, S3ConfirmDialog, S3UploadDialog, type S3UploadDialogState } from "./dialogs";

it("shows destructive S3 confirmation state and blocks a second submit while pending", async () => {
  const user = userEvent.setup();
  const onConfirm = vi.fn();
  const onClose = vi.fn();
  const value = {
    ...defaultS3ConfirmDialog,
    open: true,
    title: "Delete object",
    description: "Review the object key.",
    details: [{ label: "Object key", value: "live/file.txt" }],
    danger: true,
    action: vi.fn(async () => true),
    status: "Awaiting approval",
    error: "Previous request failed",
  };
  const { rerender } = render(<S3ConfirmDialog value={value} theme="dark" onClose={onClose} onConfirm={onConfirm} />);

  expect(screen.getByText("live/file.txt")).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Awaiting approval");
  expect(screen.getByRole("alert")).toHaveTextContent("Previous request failed");
  await user.click(screen.getByRole("button", { name: "Delete" }));
  expect(onConfirm).toHaveBeenCalledOnce();

  rerender(<S3ConfirmDialog value={{ ...value, pending: true }} theme="dark" onClose={onClose} onConfirm={onConfirm} />);
  expect(screen.getByRole("button", { name: "Working..." })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
});

it("shows a completed deletion receipt without offering another mutation when listing refresh fails", async () => {
  const onConfirm = vi.fn();
  const onClose = vi.fn();
  render(
    <S3ConfirmDialog
      value={{
        ...defaultS3ConfirmDialog,
        open: true,
        title: "S3 object deleted",
        status: "Deletion completed",
        error: "Listing refresh failed",
      }}
      theme="dark"
      onClose={onClose}
      onConfirm={onConfirm}
    />,
  );
  expect(screen.getByRole("status")).toHaveTextContent("Deletion completed");
  expect(screen.getByRole("alert")).toHaveTextContent("Listing refresh failed");
  expect(screen.queryByRole("button", { name: /Delete|Confirm|Cancel/ })).not.toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(onConfirm).not.toHaveBeenCalled();
});

it("requires an object key and content before creating a text object", () => {
  const props = uploadProps();
  const { rerender } = render(
    <S3UploadDialog {...props} value={{ ...defaultUploadDialog, open: true, mode: "text", textKey: "note.txt" }} />,
  );
  expect(screen.getByRole("button", { name: "Upload 0 object(s)" })).toBeDisabled();
  rerender(
    <S3UploadDialog {...props} value={{ ...defaultUploadDialog, open: true, mode: "text", textKey: "note.txt", textContent: "hello" }} />,
  );
  expect(screen.getByRole("button", { name: "Upload 1 object(s)" })).toBeEnabled();
});

it("edits both upload modes through the rendered controls and keeps mutation payloads explicit", async () => {
  const user = userEvent.setup();
  const file = new File(["draft"], "draft.txt", { type: "text/plain" });
  const props = uploadProps();
  function UploadEditor() {
    const [value, onChange] = useState<S3UploadDialogState>({
      ...defaultUploadDialog,
      open: true,
      files: [{ id: "draft", file, key: "draft.txt", contentType: "" }],
      error: "Earlier upload failed",
    });
    return <S3UploadDialog {...props} value={value} onChange={onChange} />;
  }
  render(<UploadEditor />);
  await user.click(screen.getByRole("button", { name: "Create text object" }));
  fireEvent.change(screen.getByPlaceholderText("notes/readme.txt"), { target: { value: "note.txt" } });
  fireEvent.change(screen.getByPlaceholderText("text/plain"), { target: { value: "text/markdown" } });
  fireEvent.change(screen.getByPlaceholderText("Text content"), { target: { value: "reviewed content" } });
  await user.click(screen.getByRole("checkbox", { name: "overwrite existing objects" }));
  expect(screen.getByRole("checkbox")).toBeChecked();
  expect(screen.getByText("reviewed content", { selector: "pre" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Upload 1 object(s)" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "File upload" }));
  fireEvent.change(screen.getByPlaceholderText("folder/subfolder/"), { target: { value: "archive/" } });
  expect(screen.getByDisplayValue("archive/draft.txt")).toBeInTheDocument();
  fireEvent.change(screen.getByDisplayValue("archive/draft.txt"), { target: { value: "archive/renamed.txt" } });
  expect(props.onUpdateFile).toHaveBeenCalledWith("draft", { key: "archive/renamed.txt" });
  await user.upload(screen.getByLabelText("Add files"), file);
  expect(props.onFiles.mock.calls[0][0]?.[0]).toBe(file);
  await user.click(screen.getByRole("button", { name: "Remove file" }));
  expect(props.onRemoveFile).toHaveBeenCalledWith("draft");
  await user.click(screen.getByRole("button", { name: "Upload 1 object(s)" }));
  expect(props.onSubmit).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(props.onClose).toHaveBeenCalledOnce();
});

function uploadProps() {
  return {
    theme: "dark",
    inputClass: "",
    borderClass: "",
    mutedClass: "",
    subtlePanelClass: "",
    onClose: vi.fn(),
    onChange: vi.fn(),
    onFiles: vi.fn(),
    onRemoveFile: vi.fn(),
    onUpdateFile: vi.fn(),
    onSubmit: vi.fn((event: FormEvent) => event.preventDefault()),
  };
}
