import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { S3ObjectBrowser } from "./object-browser";
import { S3ObjectDetailPane } from "./object-detail-pane";

const classes = { border: "border", muted: "muted", subtlePanel: "subtle", input: "input", rowHover: "hover", activeRow: "active" };
const object = { key: "backups/current.aipdb", size: 1024, last_modified: "2026-01-01T00:00:00Z" };

it("routes S3 browser controls while preserving selected object identity", async () => {
  const user = userEvent.setup();
  const callbacks = {
    onPrefixChange: vi.fn(),
    onSearchChange: vi.fn(),
    onSearch: vi.fn(),
    onBucketInfo: vi.fn(),
    onOpenTransfer: vi.fn(),
    onOpenUpload: vi.fn(),
    onRefresh: vi.fn(),
    onOpenParent: vi.fn(),
    onOpenDirectory: vi.fn(),
    onSelectObject: vi.fn(),
    onLoadMore: vi.fn(),
  };
  render(
    <S3ObjectBrowser
      target={{ config: { bucket: "backups" }, transfer_runtime_id: 4 }}
      directories={[{ name: "archive", prefix: "backups/archive/" }]}
      objects={[object]}
      prefix="backups/"
      search=""
      selectedKey={object.key}
      nextToken="next"
      latestAction={null}
      state={{ state: "idle" }}
      classes={classes}
      {...callbacks}
    />,
  );

  expect(screen.getByRole("button", { name: /backups\/current\.aipdb/i })).toHaveAttribute("aria-pressed", "true");
  fireEvent.change(screen.getByPlaceholderText("Prefix, e.g. backups/2026/"), { target: { value: "next/" } });
  fireEvent.change(screen.getByPlaceholderText("Search object keys"), { target: { value: "current" } });
  await user.click(screen.getByRole("button", { name: "Search" }));
  await user.click(screen.getByTitle("Bucket info"));
  await user.click(screen.getByTitle("Transfer files and folders"));
  await user.click(screen.getByTitle("Create a small object"));
  await user.click(screen.getByTitle("Refresh objects"));
  await user.click(screen.getByRole("button", { name: /bucket root/i }));
  await user.click(screen.getByRole("button", { name: /archive/i }));
  await user.click(screen.getByRole("button", { name: /backups\/current\.aipdb/i }));
  await user.click(screen.getByRole("button", { name: "Load more" }));

  expect(callbacks.onPrefixChange).toHaveBeenCalledWith("next/");
  expect(callbacks.onSearchChange).toHaveBeenCalledWith("current");
  expect(callbacks.onSearch).toHaveBeenCalledOnce();
  expect(callbacks.onBucketInfo).toHaveBeenCalledOnce();
  expect(callbacks.onOpenTransfer).toHaveBeenCalledOnce();
  expect(callbacks.onOpenUpload).toHaveBeenCalledOnce();
  expect(callbacks.onRefresh).toHaveBeenCalledOnce();
  expect(callbacks.onOpenParent).toHaveBeenCalledOnce();
  expect(callbacks.onOpenDirectory).toHaveBeenCalledWith("backups/archive/");
  expect(callbacks.onSelectObject).toHaveBeenCalledWith(object.key);
  expect(callbacks.onLoadMore).toHaveBeenCalledOnce();
});

it("keeps S3 object actions controlled by the detail owner", async () => {
  const user = userEvent.setup();
  const callbacks = {
    onMetadataSearch: vi.fn(),
    onOpenLifecycle: vi.fn(),
    onOpenPresign: vi.fn(),
    onOpenVersions: vi.fn(),
    onDownload: vi.fn(),
    onDelete: vi.fn(),
  };
  render(
    <S3ObjectDetailPane
      active
      selectedKey={object.key}
      selectedObject={object}
      metadata={null}
      directories={[]}
      objects={[object]}
      visibleBytes={1024}
      prefix="backups/"
      search=""
      metadataSearch=""
      state={{ state: "idle", error: "" }}
      classes={classes}
      {...callbacks}
    />,
  );

  await user.click(screen.getByTitle("Bucket lifecycle"));
  await user.click(screen.getByTitle("Create temporary S3 URL"));
  await user.click(screen.getByTitle("Object versions"));
  await user.click(screen.getByTitle("Download object"));
  await user.click(screen.getByTitle("Delete object"));
  expect(callbacks.onOpenLifecycle).toHaveBeenCalledOnce();
  expect(callbacks.onOpenPresign).toHaveBeenCalledOnce();
  expect(callbacks.onOpenVersions).toHaveBeenCalledOnce();
  expect(callbacks.onDownload).toHaveBeenCalledOnce();
  expect(callbacks.onDelete).toHaveBeenCalledOnce();
});
