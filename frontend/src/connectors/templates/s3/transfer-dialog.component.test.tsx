import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { FileTransferDialog } from "../../../components/file-transfer/file-transfer-dialog";
import { apiPost, apiPostForm } from "../../../lib/api";
import { joinTransferPath, normalizeTransferDirectory } from "./transfer-paths";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPostForm: vi.fn(), apiGet: vi.fn(), apiDownload: vi.fn() }));

function deferred<T>() {
  let resolve: (_value: T) => void = () => {};
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

beforeEach(() => {
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPostForm).mockReset();
});

it("retains folder-relative identity in preview and multipart after prefix navigation", async () => {
  const user = userEvent.setup();
  vi.mocked(apiPost).mockResolvedValue({ path: "/next// ", parent: "/", entries: [] });
  vi.mocked(apiPostForm).mockResolvedValue({ id: 1, status: "completed", direction: "upload", items: [] });
  render(
    <FileTransferDialog
      open
      runtimeTarget={{ id: 7, name: "objects" }}
      onClose={() => {}}
      options={{
        defaultDirectory: "/",
        recursive: true,
        folderUpload: true,
        joinRemotePath: joinTransferPath,
        normalizeRemoteDirectoryInput: normalizeTransferDirectory,
      }}
    />,
  );
  const file = new File(["data"], " invoice ", { type: "text/plain" });
  Object.defineProperty(file, "webkitRelativePath", { value: "folder// invoice " });
  const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (!fileInput) throw new Error("File input is missing");
  fireEvent.change(fileInput, { target: { files: [file] } });
  fireEvent.change(screen.getByLabelText("Remote folder"), { target: { value: "/prefix//" } });
  expect(screen.getByText("/prefix//folder// invoice ", { normalizer: (value) => value })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Browse" }));
  expect(vi.mocked(apiPost)).toHaveBeenCalledWith(
    "/api/file-transfers/browse",
    { runtime_id: 7, path: "/prefix//" },
    { signal: expect.any(AbortSignal) },
  );
  await user.click(await screen.findByRole("button", { name: "Use this folder" }));
  expect(screen.getByText("/next// /folder// invoice ", { normalizer: (value) => value })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Start upload/ }));
  await screen.findByRole("button", { name: "Clear" });
  const form = vi.mocked(apiPostForm).mock.calls[0]?.[1];
  if (!(form instanceof FormData)) throw new Error("Upload form is missing");
  expect(form.get("remote_dir")).toBe("/next// ");
  expect(JSON.parse(String(form.get("relative_paths")))).toEqual(["folder// invoice "]);
});

it("waits for the canonical browse path before using an upload folder", async () => {
  const user = userEvent.setup();
  const pending = deferred<{ path: string; parent: string; entries: never[] }>();
  vi.mocked(apiPost).mockReturnValue(pending.promise);
  render(
    <FileTransferDialog
      open
      runtimeTarget={{ id: 7, name: "objects" }}
      onClose={() => {}}
      options={{
        defaultDirectory: "/",
        recursive: true,
        folderUpload: true,
        joinRemotePath: joinTransferPath,
        normalizeRemoteDirectoryInput: normalizeTransferDirectory,
      }}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Browse" }));
  expect(screen.getByRole("button", { name: "Use this folder" })).toBeDisabled();
  pending.resolve({ path: "/canonical/", parent: "/", entries: [] });

  expect(await screen.findByRole("button", { name: "Use this folder" })).toBeEnabled();
});
