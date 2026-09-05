import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { FileTransferDialog } from "../../../components/file-transfer/file-transfer-dialog";
import { apiPost, apiPostForm } from "../../../lib/api";
import { joinTransferPath, normalizeTransferDirectory } from "./transfer-paths";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), apiPostForm: vi.fn(), apiGet: vi.fn(), apiDownload: vi.fn() }));

it("retains folder-relative identity in preview and multipart after prefix navigation", async () => {
  const user = userEvent.setup();
  apiPost.mockResolvedValue({ path: "/next// ", parent: "/", entries: [] });
  apiPostForm.mockResolvedValue({ id: 1, status: "completed", direction: "upload", items: [] });
  const { container } = render(
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
  fireEvent.change(container.querySelector('input[type="file"]'), { target: { files: [file] } });
  fireEvent.change(screen.getByLabelText("Remote folder"), { target: { value: "/prefix//" } });
  expect(screen.getByText("/prefix//folder// invoice ", { normalizer: (value) => value })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Browse" }));
  expect(apiPost).toHaveBeenCalledWith("/api/file-transfers/browse", { runtime_id: 7, path: "/prefix//" });
  await user.click(await screen.findByRole("button", { name: "Use this folder" }));
  expect(screen.getByText("/next// /folder// invoice ", { normalizer: (value) => value })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Start upload/ }));
  const form = apiPostForm.mock.calls[0][1];
  expect(form.get("remote_dir")).toBe("/next// ");
  expect(JSON.parse(form.get("relative_paths"))).toEqual(["folder// invoice "]);
});
