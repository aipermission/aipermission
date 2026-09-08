import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { RemoteBrowserDialog } from "./file-transfer-browser-dialog";

const baseBrowser = {
  open: true,
  purpose: "download",
  path: "/srv",
  state: "ready",
  data: {
    path: "/srv",
    parent: "/",
    entries: [
      { type: "file", name: "app.log", path: "/srv/app.log", size: 12, modified_at: "2026-09-08T00:00:00Z" },
      { type: "directory", name: "archive", path: "/srv/archive", modified_at: "2026-09-08T00:00:00Z" },
    ],
    has_more: true,
    next_cursor: "next-page",
  },
};

it("selects remote files, submits them once, and supports paginated browsing", async () => {
  const user = userEvent.setup();
  const onAddFiles = vi.fn(async () => true);
  const onClose = vi.fn();
  const onLoad = vi.fn();
  render(
    <RemoteBrowserDialog
      browser={baseBrowser}
      onClose={onClose}
      onLoad={onLoad}
      onPathChange={vi.fn()}
      onUseDirectory={vi.fn()}
      onAddFiles={onAddFiles}
      queuedPaths={new Set()}
      recursive
    />,
  );

  await user.click(screen.getByRole("checkbox", { name: "Select app.log" }));
  await user.click(screen.getByRole("button", { name: "Add Selected Files (1)" }));
  expect(onAddFiles).toHaveBeenCalledWith([expect.objectContaining({ path: "/srv/app.log" })]);
  expect(onClose).toHaveBeenCalledOnce();

  await user.click(screen.getByRole("button", { name: "Load more" }));
  expect(onLoad).toHaveBeenCalledWith("/srv", "download", { append: true, cursor: "next-page" });
});

it("uses only a ready upload directory and reloads paths from keyboard", async () => {
  const user = userEvent.setup();
  const onLoad = vi.fn();
  const onUseDirectory = vi.fn();
  const onPathChange = vi.fn();
  render(
    <RemoteBrowserDialog
      browser={{ ...baseBrowser, purpose: "upload" }}
      onClose={vi.fn()}
      onLoad={onLoad}
      onPathChange={onPathChange}
      onUseDirectory={onUseDirectory}
      onAddFiles={vi.fn()}
      queuedPaths={new Set()}
      recursive={false}
    />,
  );

  const path = screen.getByDisplayValue("/srv");
  await user.clear(path);
  await user.type(path, "/tmp{Enter}");
  expect(onPathChange).toHaveBeenCalled();
  expect(onLoad).toHaveBeenCalledWith("/srv", "upload");
  await user.click(screen.getByRole("button", { name: "Use this folder" }));
  expect(onUseDirectory).toHaveBeenCalledWith("/srv");
});
