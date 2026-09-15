import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3VersionsDialog } from "./versions-dialog";

const version = { version_id: "version-1", is_latest: true, delete_marker: false, size: 12 };

function renderVersionDialog(actionResponse) {
  const onRun = vi.fn(async ({ actionName }) =>
    actionName === "list_object_versions" ? { output: { versions: [version], next_cursor: "" } } : actionResponse(),
  );
  render(
    <S3VersionsDialog
      open
      objectKey="file.txt"
      theme="dark"
      borderClass="border-stone-700"
      mutedClass="text-stone-500"
      onClose={vi.fn()}
      onRun={onRun}
    />,
  );
  return onRun;
}

it("keeps version deletion errors in the active confirmation dialog", async () => {
  const user = userEvent.setup();
  renderVersionDialog(() => Promise.reject(new Error("version denied")));
  await user.click(await screen.findByTitle("Delete this version"));
  await user.click(screen.getByRole("button", { name: "Delete version" }));

  expect(await screen.findByRole("alert")).toHaveTextContent("version denied");
  expect(screen.getByRole("button", { name: "Delete version" })).toBeEnabled();
});

it("shows pending approval without allowing another version deletion", async () => {
  const user = userEvent.setup();
  const onRun = renderVersionDialog(() => Promise.resolve(null));
  await user.click(await screen.findByTitle("Delete this version"));
  await user.click(screen.getByRole("button", { name: "Delete version" }));

  expect(await screen.findByRole("status")).toHaveTextContent("pending");
  expect(screen.getByRole("button", { name: "Delete version" })).toBeDisabled();
  await waitFor(() => expect(onRun).toHaveBeenCalledTimes(2));
});
