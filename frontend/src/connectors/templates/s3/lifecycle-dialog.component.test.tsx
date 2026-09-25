import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3LifecycleDialog } from "./lifecycle-dialog";

it("requires explicit acknowledgement before replacing the complete lifecycle policy", async () => {
  const user = userEvent.setup();
  const onRun = vi.fn(async ({ actionName }: { actionName: string }) =>
    actionName === "get_bucket_lifecycle"
      ? { output: { configured: true, rules: [], raw_xml: "<LifecycleConfiguration/>" } }
      : { output: {} },
  );
  render(
    <S3LifecycleDialog open bucket="test-bucket" theme="dark" inputClass="" borderClass="" mutedClass="" onClose={vi.fn()} onRun={onRun} />,
  );
  await screen.findByText("<LifecycleConfiguration/>");
  expect(screen.getByRole("button", { name: "Replace lifecycle policy" })).toBeDisabled();
  await user.click(screen.getByRole("checkbox", { name: /replaces the complete lifecycle policy/i }));
  await user.click(screen.getByRole("button", { name: "Replace lifecycle policy" }));
  await waitFor(() =>
    expect(onRun).toHaveBeenCalledWith(
      expect.objectContaining({
        actionName: "replace_bucket_lifecycle",
        input: expect.objectContaining({ rule_id: "aipermission-expiration", abort_incomplete_multipart_days: 7 }),
      }),
    ),
  );
});
