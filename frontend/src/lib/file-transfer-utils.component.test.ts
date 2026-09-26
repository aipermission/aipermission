import assert from "node:assert/strict";
import { afterEach, expect, expectTypeOf, test, vi } from "vitest";

import {
  defaultRemoteDirectory,
  fileTransferFailureText,
  fileTransferPathPolicy,
  forgetDownloadPath,
  formatBytes,
  formatETA,
  formatShortDate,
  joinRemotePath,
  localFileID,
  mergeUploadQueue,
  normalizeRemoteDirectoryInput,
  pendingBatchItemIDs,
  rememberDownloadPath,
  rememberedDownloadPath,
  relocateUploadQueue,
  suggestedArchiveName,
  transferProgress,
} from "./file-transfer-utils";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

test("transferProgress treats canceled terminal queue items as processed", () => {
  const progress = transferProgress({
    status: "canceled",
    size_bytes: 100,
    transferred_bytes: 40,
  });

  assert.equal(progress.percent, 100);
  assert.equal(progress.label, "40 B / 100 B");
});

test("transferProgress keeps running queues byte-based", () => {
  const progress = transferProgress({
    status: "running",
    size_bytes: 100,
    transferred_bytes: 40,
  });

  assert.equal(progress.percent, 40);
});

test("file transfer outcome uncertainty warns before retry", () => {
  assert.match(
    fileTransferFailureText({ failure_kind: "outcome_unknown", error: "internal detail" }),
    /Inspect the destination before retrying/,
  );
  assert.equal(fileTransferFailureText({ failure_kind: "timeout", error: "timed out" }), "timed out");
});

test("transfer feedback handles absent, unknown-size, over-complete, and terminal items", () => {
  expect(transferProgress(null)).toEqual({ percent: 0, label: "" });
  expect(transferProgress({ transferred_bytes: 10 })).toEqual({ percent: 0, label: "10 B transferred" });
  expect(transferProgress({ status: "running", size_bytes: 10, transferred_bytes: 20 }).percent).toBe(100);
  expect(transferProgress({ status: "completed" }).percent).toBe(100);
  expect(fileTransferFailureText(null, "fallback")).toBe("fallback");
  expect(fileTransferFailureText({}, "fallback")).toBe("fallback");
});

test("formats byte sizes and ETA values across all supported units", () => {
  for (const [bytes, formatted] of [
    [0, "0 B"],
    [1024, "1.00 KiB"],
    [10240, "10.0 KiB"],
    [1048576, "1.00 MiB"],
    [1073741824, "1.00 GiB"],
    [1099511627776, "1.00 TiB"],
  ]) {
    expect(formatBytes(bytes)).toBe(formatted);
  }
  expect(formatBytes(undefined)).toBe("0 B");
  expect(formatETA(undefined)).toBe("-");
  expect(formatETA(-1)).toBe("-");
  expect(formatETA(12.4)).toBe("12s");
  expect(formatETA(125)).toBe("2m 5s");
  expect(formatShortDate(null)).toBe("");
  expect(formatShortDate("invalid")).toBe("");
  expect(formatShortDate("2026-09-26T12:00:00Z")).toBe(
    new Date("2026-09-26T12:00:00Z").toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }),
  );
});

test("preserves typed queue extensions during replacement and relocation", () => {
  const original = { name: "old.txt", remote_path: "/data/one", secretSafeExtension: { marker: 1 } };
  const replacement = { name: "new.txt", remote_path: "/data/one", secretSafeExtension: { marker: 2 } };
  const added = { name: "second.txt", remote_path: "/data/two", secretSafeExtension: { marker: 3 } };
  expect(mergeUploadQueue([original], [replacement, added])).toEqual([replacement, added]);
  expect(mergeUploadQueue(undefined, undefined)).toEqual([]);
  expect(mergeUploadQueue(null, [added])[0]).toBe(added);
  const merged = mergeUploadQueue([original], [replacement, added, original]);
  expect(merged).toEqual([original, added]);
  expect([original, replacement, added].map((item) => item.remote_path)).toEqual(["/data/one", "/data/one", "/data/two"]);
  expectTypeOf(merged[0].secretSafeExtension.marker).toEqualTypeOf<number>();
  expect(relocateUploadQueue([replacement], "/new")).toEqual([{ ...replacement, remote_path: "/new/new.txt" }]);
  expect(relocateUploadQueue([{ ...replacement, relative_path: "folder/new.txt" }], "/new")[0].remote_path).toBe("/new/folder/new.txt");
  const opaqueJoin = vi.fn((directory: string, name: string) => `${directory}${name}`);
  const relocated = relocateUploadQueue([{ ...replacement, relative_path: "../ key " }], "//prefix// ", opaqueJoin);
  expect(opaqueJoin).toHaveBeenCalledWith("//prefix// ", "../ key ");
  expect(relocated[0].remote_path).toBe("//prefix// ../ key ");
  expectTypeOf(relocated[0].secretSafeExtension.marker).toEqualTypeOf<number>();
  expect(replacement.remote_path).toBe("/data/one");
  expect(pendingBatchItemIDs(null)).toEqual([]);
  expect(
    pendingBatchItemIDs({
      items: [
        { id: "12", status: "pending" },
        { id: 13, status: "completed" },
      ],
    }),
  ).toEqual([12]);
});

test("uses supplied transfer path policies and handles inaccessible local storage", () => {
  expect(defaultRemoteDirectory()).toBe("/home");
  expect(normalizeRemoteDirectoryInput(" data/// ")).toBe("/data");
  expect(normalizeRemoteDirectoryInput("")).toBe("/");
  expect(joinRemotePath("/", "/ file ")).toBe("/ file");
  expect(fileTransferPathPolicy({}).joinRemotePath("/data/", "file")).toBe("/data/file");
  const normalize = vi.fn((value: string) => value);
  const join = vi.fn((directory: string, name: string) => `${directory}::${name}`);
  expect(fileTransferPathPolicy({ joinRemotePath: join, normalizeRemoteDirectoryInput: normalize }).joinRemotePath).toBe(join);
  rememberDownloadPath({ id: 4 }, "//opaque// ", normalize);
  expect(rememberedDownloadPath({ id: 4 }, "/fallback", normalize)).toBe("//opaque// ");
  forgetDownloadPath({ id: 4 });
  expect(rememberedDownloadPath({ id: 4 }, "/fallback", normalize)).toBe("/fallback");
  expect(rememberedDownloadPath(null, "")).toBe("/home");
  rememberDownloadPath(null, "/ignored");
  forgetDownloadPath(null);
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("storage denied");
  });
  expect(rememberedDownloadPath({ id: 4 }, "/fallback")).toBe("/fallback");
  vi.stubGlobal("window", undefined);
  expect(rememberedDownloadPath({ id: 4 }, "/fallback")).toBe("/fallback");
  rememberDownloadPath({ id: 4 }, "/ignored");
  forgetDownloadPath({ id: 4 });
});

test("generates local file IDs and timestamped archive names without changing inputs", () => {
  vi.spyOn(Math, "random").mockReturnValue(0.5);
  expect(localFileID({ name: "data.bin", size: 12, lastModified: 34 })).toBe("data.bin-12-34-8");
  vi.useFakeTimers();
  try {
    vi.setSystemTime(new Date("2026-09-26T12:34:56Z"));
    expect(suggestedArchiveName()).toBe("aipermission-download-2026-09-26T12-34-56.zip");
  } finally {
    vi.useRealTimers();
  }
});
