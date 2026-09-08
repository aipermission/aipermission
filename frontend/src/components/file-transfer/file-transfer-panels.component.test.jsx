import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { TransferQueuePanel, TransferSetupPanel } from "./file-transfer-panels";

const idleBatch = { state: "idle", item: null, error: null };

it("renders connector-neutral upload controls and preserves recursive folder selection", async () => {
  const user = userEvent.setup();
  const fileInputRef = createRef();
  const folderInputRef = createRef();
  const onModeChange = vi.fn();
  const onOpenBrowser = vi.fn();
  const onLocalFileChange = vi.fn();
  const onRemoteDirectoryChange = vi.fn();
  render(
    <TransferSetupPanel
      runtimeTarget={{ name: "My connector", subtitle: "profile" }}
      mode="upload"
      batch={idleBatch}
      activeBatch={null}
      queue={[]}
      progress={{ percent: 0, processed: 0, total: 0, bytes: 0 }}
      notice={null}
      transferNotice="Transfer policy"
      remoteDir="/tmp"
      defaultRemoteDir="/home"
      recursive
      fileInputRef={fileInputRef}
      folderInputRef={folderInputRef}
      onModeChange={onModeChange}
      onRemoteDirectoryChange={onRemoteDirectoryChange}
      onOpenBrowser={onOpenBrowser}
      onLocalFileChange={onLocalFileChange}
    />,
  );

  expect(screen.getByText("My connector")).toBeInTheDocument();
  expect(screen.getByText("Transfer policy")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Upload" }));
  expect(onModeChange).toHaveBeenCalledWith("upload");
  await user.type(screen.getByPlaceholderText("/home"), "/incoming");
  expect(onRemoteDirectoryChange).toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Add files" }));
  fireEvent.change(fileInputRef.current, { target: { files: [new File(["data"], "one.txt")] } });
  expect(onLocalFileChange).toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Download" }));
  expect(onModeChange).toHaveBeenCalledWith("download");
  await user.click(screen.getByRole("button", { name: "Browse" }));
  expect(onOpenBrowser).toHaveBeenCalledWith("upload");
  await user.click(screen.getByRole("button", { name: "Add folder" }));
  expect(folderInputRef.current).toHaveAttribute("webkitdirectory");
});

it("opens the remote browser in download mode", async () => {
  const onOpenBrowser = vi.fn();
  render(
    <TransferSetupPanel
      runtimeTarget={{ name: "My connector", subtitle: "profile" }}
      mode="download"
      batch={idleBatch}
      activeBatch={null}
      queue={[]}
      progress={{ percent: 0, processed: 0, total: 0, bytes: 0 }}
      transferNotice="Transfer policy"
      remoteDir="/tmp"
      defaultRemoteDir="/home"
      recursive={false}
      fileInputRef={createRef()}
      folderInputRef={createRef()}
      onModeChange={vi.fn()}
      onRemoteDirectoryChange={vi.fn()}
      onOpenBrowser={onOpenBrowser}
      onLocalFileChange={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Add remote files" }));
  expect(onOpenBrowser).toHaveBeenCalledWith("download");
});

it("renders only the commands valid for the current batch state", async () => {
  const user = userEvent.setup();
  const actions = {
    onRefresh: vi.fn(),
    onRemove: vi.fn(),
    onMove: vi.fn(),
    onPause: vi.fn(),
    onResume: vi.fn(),
    onCancel: vi.fn(),
    onSaveDownload: vi.fn(),
    onClear: vi.fn(),
    onStart: vi.fn(),
  };
  const { rerender } = render(
    <TransferQueuePanel
      mode="download"
      queue={[]}
      batch={{ state: "ready", item: { id: 2, direction: "download", status: "completed", items: [] }, error: null }}
      activeBatch={false}
      canStart={false}
      {...actions}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Save download" }));
  await user.click(screen.getByRole("button", { name: "Clear" }));
  expect(actions.onSaveDownload).toHaveBeenCalledOnce();
  expect(actions.onClear).toHaveBeenCalledOnce();
  expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument();

  rerender(
    <TransferQueuePanel
      mode="upload"
      queue={[{ id: 1, name: "a.txt", status: "running" }]}
      batch={{ state: "ready", item: { id: 3, direction: "upload", status: "running", items: [] }, error: null }}
      activeBatch
      canStart={false}
      {...actions}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Pause" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(actions.onPause).toHaveBeenCalledOnce();
  expect(actions.onCancel).toHaveBeenCalledOnce();
  expect(screen.queryByRole("button", { name: "Save download" })).not.toBeInTheDocument();

  rerender(
    <TransferQueuePanel
      mode="upload"
      queue={[{ id: 1, name: "a.txt", status: "paused" }]}
      batch={{ state: "ready", item: { id: 3, direction: "upload", status: "paused", items: [] }, error: null }}
      activeBatch
      canStart={false}
      {...actions}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Resume" }));
  expect(actions.onResume).toHaveBeenCalledOnce();
});

it("keeps the transfer mode fixed while a batch start is pending", async () => {
  const user = userEvent.setup();
  const onModeChange = vi.fn();
  render(
    <TransferSetupPanel
      runtimeTarget={{ name: "My connector", subtitle: "profile" }}
      mode="upload"
      batch={{ state: "starting", item: null, error: null }}
      activeBatch={false}
      queue={[]}
      progress={{ percent: 0, processed: 0, total: 0, bytes: 0 }}
      notice={null}
      transferNotice="Transfer policy"
      remoteDir="/tmp"
      defaultRemoteDir="/home"
      recursive={false}
      fileInputRef={createRef()}
      folderInputRef={createRef()}
      onModeChange={onModeChange}
      onRemoteDirectoryChange={vi.fn()}
      onOpenBrowser={vi.fn()}
      onLocalFileChange={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: "Upload" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Download" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Download" }));
  expect(onModeChange).not.toHaveBeenCalled();
});
