import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3PresignDialog } from "./presign-dialog";

it.each([" /a//../file ", " ", "/file", "caf\u00e9", "cafe\u0301"])("presigns the selected opaque key %j", async (key) => {
  const onRun = vi.fn().mockResolvedValue(null);
  render(<S3PresignDialog open selectedKey={key} onRun={onRun} onClose={() => {}} />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Create URL" }));
  expect(onRun).toHaveBeenCalledWith(expect.objectContaining({ actionName: "presign_download", input: { key, expires_seconds: 900 } }));
});
