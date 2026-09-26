import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3PresignDialog } from "./presign-dialog";
import { S3LifecycleDialog } from "./lifecycle-dialog";
import { S3VersionsDialog } from "./versions-dialog";

function deferred() {
  let resolve: (_item: { output?: unknown } | null) => void = () => {};
  let reject: (_error: unknown) => void = () => {};
  const promise = new Promise<{ output?: unknown } | null>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}
const common = { open: true, theme: "dark", inputClass: "", borderClass: "", mutedClass: "", onClose: vi.fn() };

it.each(["resolve", "reject"] as const)("ignores stale presign %s without unlocking a new request", async (settlement) => {
  const user = userEvent.setup();
  const previous = deferred();
  const current = deferred();
  const onRun = vi.fn().mockReturnValueOnce(previous.promise).mockReturnValueOnce(current.promise);
  const view = render(<S3PresignDialog {...common} scopeKey="first" selectedKey="file" onRun={onRun} />);
  await user.click(screen.getByRole("button", { name: "Create URL" }));
  view.rerender(<S3PresignDialog {...common} scopeKey="second" selectedKey="file" onRun={onRun} />);
  await user.click(screen.getByRole("button", { name: "Create URL" }));
  await act(async () => {
    if (settlement === "resolve") previous.resolve({ output: { url: "https://example.test/old" } });
    else previous.reject(new Error("old error"));
  });
  expect(screen.getByRole("button", { name: "Creating..." })).toBeDisabled();
  expect(screen.queryByText("https://example.test/old")).not.toBeInTheDocument();
  expect(screen.queryByText("old error")).not.toBeInTheDocument();
  await act(async () => current.resolve({ output: { url: "https://example.test/new" } }));
  expect(screen.getByText("https://example.test/new")).toBeVisible();
  expect(screen.getByRole("button", { name: "Create URL" })).toBeEnabled();
});

it("does not release a newer lifecycle read when an old bucket read settles", async () => {
  const previous = deferred();
  const current = deferred();
  const onRun = vi.fn().mockReturnValueOnce(previous.promise).mockReturnValueOnce(current.promise);
  const view = render(<S3LifecycleDialog {...common} scopeKey="first" bucket="bucket" onRun={onRun} />);
  await waitFor(() => expect(onRun).toHaveBeenCalledOnce());
  view.rerender(<S3LifecycleDialog {...common} scopeKey="second" bucket="bucket" onRun={onRun} />);
  await waitFor(() => expect(onRun).toHaveBeenCalledTimes(2));
  await act(async () => previous.resolve({ output: { configured: true, raw_xml: "old policy" } }));
  expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
  expect(screen.queryByText("old policy")).not.toBeInTheDocument();
  await act(async () => current.resolve({ output: { configured: false } }));
  expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled();
});

it("does not unlock a new version read when the previous scope settles", async () => {
  const previous = deferred();
  const current = deferred();
  const onRun = vi.fn().mockReturnValueOnce(previous.promise).mockReturnValueOnce(current.promise);
  const view = render(<S3VersionsDialog {...common} scopeKey="first" objectKey="file" onRun={onRun} />);
  await waitFor(() => expect(onRun).toHaveBeenCalledOnce());
  view.rerender(<S3VersionsDialog {...common} scopeKey="second" objectKey="file" onRun={onRun} />);
  await waitFor(() => expect(onRun).toHaveBeenCalledTimes(2));
  await act(async () => previous.resolve({ output: { versions: [{ version_id: "old-version" }] } }));
  expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
  expect(screen.queryByText("old-version")).not.toBeInTheDocument();
  await act(async () => current.resolve({ output: { versions: [{ version_id: "new-version" }] } }));
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(screen.getByText("new-version")).toBeVisible();
});

it("does not run old-target refresh callbacks after a version mutation reload is retired", async () => {
  const user = userEvent.setup();
  const reload = deferred();
  let listCount = 0;
  const version = { version_id: "version-1", delete_marker: false };
  const onRun = vi.fn(async ({ actionName }: { actionName: string }) => {
    if (actionName !== "list_object_versions") return { output: {} };
    listCount += 1;
    if (listCount === 2) return reload.promise;
    return { output: { versions: [version] } };
  });
  const onChanged = vi.fn();
  const view = render(<S3VersionsDialog {...common} scopeKey="first" objectKey="file" onRun={onRun} onChanged={onChanged} />);
  await user.click(await screen.findByTitle("Delete this version"));
  await user.click(screen.getByRole("button", { name: "Delete version" }));
  await waitFor(() => expect(listCount).toBe(2));
  view.rerender(<S3VersionsDialog {...common} scopeKey="second" objectKey="file" onRun={onRun} onChanged={onChanged} />);
  await waitFor(() => expect(listCount).toBe(3));
  await act(async () => reload.resolve({ output: { versions: [{ version_id: "old-view" }] } }));
  expect(onChanged).not.toHaveBeenCalled();
  expect(screen.queryByText("old-view")).not.toBeInTheDocument();
  expect(screen.getByText("version-1")).toBeVisible();
});
