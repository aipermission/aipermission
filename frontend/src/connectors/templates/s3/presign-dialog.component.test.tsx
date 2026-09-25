import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3PresignDialog } from "./presign-dialog";

it.each([" /a//../file ", " ", "/file", "caf\u00e9", "cafe\u0301"])("presigns the selected opaque key %j", async (key) => {
  const onRun = vi.fn().mockResolvedValue(null);
  render(
    <S3PresignDialog open selectedKey={key} theme="dark" inputClass="" borderClass="" mutedClass="" onRun={onRun} onClose={() => {}} />,
  );
  await userEvent.setup().click(screen.getByRole("button", { name: "Create URL" }));
  expect(onRun).toHaveBeenCalledWith(expect.objectContaining({ actionName: "presign_download", input: { key, expires_seconds: 900 } }));
});

it("renders upload headers only after a successful presign request", async () => {
  const user = userEvent.setup();
  const onRun = vi.fn().mockResolvedValue({
    output: {
      url: "https://example.test/signed",
      operation: "upload",
      expires_at: "tomorrow",
      required_headers: { "Content-Type": "text/plain" },
    },
  });
  render(
    <S3PresignDialog
      open
      selectedKey="note.txt"
      theme="dark"
      inputClass=""
      borderClass=""
      mutedClass=""
      onRun={onRun}
      onClose={() => {}}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Upload" }));
  await user.click(screen.getByRole("button", { name: "Create URL" }));
  expect(onRun).toHaveBeenCalledWith(
    expect.objectContaining({ actionName: "presign_upload", input: { key: "note.txt", expires_seconds: 900, overwrite: false } }),
  );
  expect(await screen.findByText("https://example.test/signed")).toBeInTheDocument();
  expect(screen.getByText("Content-Type: text/plain")).toBeInTheDocument();
});
