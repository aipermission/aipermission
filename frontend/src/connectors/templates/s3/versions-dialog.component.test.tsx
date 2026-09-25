import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3VersionsDialog } from "./versions-dialog";

const version = { version_id: "version-1", is_latest: true, delete_marker: false, size: 12 };

function renderVersionDialog(actionResponse: () => Promise<null>) {
  const onRun = vi.fn(async ({ actionName }: { actionName: string }) =>
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
      onChanged={vi.fn()}
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

it("includes the current ETag guard when restoring an older version", async () => {
  const user = userEvent.setup();
  const onRun = vi.fn(async ({ actionName }: { actionName: string }) => {
    if (actionName === "list_object_versions") return { output: { versions: [version] } };
    if (actionName === "get_object_metadata") return { output: { etag: "etag-current" } };
    return { output: {} };
  });
  render(
    <S3VersionsDialog
      open
      objectKey="file.txt"
      theme="dark"
      borderClass="border-stone-700"
      mutedClass="text-stone-500"
      onClose={vi.fn()}
      onRun={onRun}
      onChanged={vi.fn()}
    />,
  );
  await user.click(await screen.findByTitle("Restore this version"));
  await user.click(screen.getByRole("button", { name: "Restore version" }));
  await waitFor(() =>
    expect(onRun).toHaveBeenCalledWith(
      expect.objectContaining({
        actionName: "restore_object_version",
        input: { key: "file.txt", version_id: "version-1", expected_current_etag: "etag-current" },
      }),
    ),
  );
});

it("appends version pages and never restores a delete marker or deletes on cancellation", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  const onRun = vi
    .fn()
    .mockResolvedValueOnce({ output: { versions: [version], next_cursor: "page-2" } })
    .mockResolvedValueOnce({ output: { versions: [{ version_id: "marker-2", delete_marker: true, is_latest: false }], next_cursor: "" } });
  render(
    <S3VersionsDialog
      open
      objectKey="file.txt"
      theme="light"
      borderClass="border-stone-200"
      mutedClass="text-stone-500"
      onClose={onClose}
      onRun={onRun}
    />,
  );
  await screen.findByTitle("version-1");
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await screen.findByTitle("marker-2");
  expect(screen.getByTitle("version-1")).toBeVisible();
  expect(screen.getAllByTitle("Restore this version")[1]).toBeDisabled();
  expect(screen.getByRole("button", { name: "Load more" })).toBeDisabled();
  expect(onRun).toHaveBeenLastCalledWith(
    expect.objectContaining({ actionName: "list_object_versions", input: { key: "file.txt", cursor: "page-2", limit: 100 } }),
  );
  await user.click(screen.getAllByTitle("Delete this version")[1]);
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog", { name: "Delete object version" })).not.toBeInTheDocument();
  expect(onRun).toHaveBeenCalledTimes(2);
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalledOnce();
});

it("restores a version with an absence guard only after a verified not-found precondition", async () => {
  const user = userEvent.setup();
  const onChanged = vi.fn();
  const onRun = vi.fn(async ({ actionName }: { actionName: string }) => {
    if (actionName === "list_object_versions") return { output: { versions: [version] } };
    if (actionName === "get_object_metadata")
      throw Object.assign(new Error("Missing object"), { actionItem: { output: { code: "not_found" } } });
    return { output: {} };
  });
  render(
    <S3VersionsDialog
      open
      objectKey="file.txt"
      theme="dark"
      borderClass="border-stone-700"
      mutedClass="text-stone-500"
      onClose={vi.fn()}
      onRun={onRun}
      onChanged={onChanged}
    />,
  );
  await user.click(await screen.findByTitle("Restore this version"));
  expect(onRun).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "Restore version" }));
  await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
  expect(onRun).toHaveBeenNthCalledWith(
    3,
    expect.objectContaining({
      actionName: "restore_object_version",
      input: { key: "file.txt", version_id: "version-1", expected_current_absent: true },
    }),
  );
  expect(onRun).toHaveBeenNthCalledWith(
    4,
    expect.objectContaining({ actionName: "list_object_versions", input: { key: "file.txt", cursor: "", limit: 100 } }),
  );
  expect(screen.queryByRole("dialog", { name: "Restore object version" })).not.toBeInTheDocument();
});

it("refuses restore if the current object cannot be verified and preserves the confirmation for correction", async () => {
  const user = userEvent.setup();
  const onRun = vi.fn(async ({ actionName }: { actionName: string }) => {
    if (actionName === "list_object_versions") return { output: { versions: [version] } };
    throw new Error("Metadata unavailable");
  });
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
  await user.click(await screen.findByTitle("Restore this version"));
  await user.click(screen.getByRole("button", { name: "Restore version" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Metadata unavailable");
  expect(screen.getByRole("button", { name: "Restore version" })).toBeEnabled();
  expect(onRun).toHaveBeenCalledTimes(2);
  expect(onRun.mock.calls.map(([request]) => request.actionName)).toEqual(["list_object_versions", "get_object_metadata"]);
});
