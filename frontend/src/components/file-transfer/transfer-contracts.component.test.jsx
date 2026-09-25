import { expect, it } from "vitest";
import { remoteExpansionEntries, transferBatchResponse } from "./transfer-contracts";

const batch = {
  id: 7,
  status: "paused",
  direction: "download",
  archive_name: "logs.zip",
  items: [{ id: 8, status: "pending", file_name: "a" }],
};

it("preserves validated batches and file/directory expansion entries", () => {
  expect(transferBatchResponse(batch)).toEqual(batch);
  expect(transferBatchResponse({ id: 7, status: "completed", direction: "upload" })).toEqual({
    id: 7,
    status: "completed",
    direction: "upload",
  });
  const entries = [
    { type: "file", path: "/a", name: "a", size: 0 },
    { type: "directory", path: "/folder", name: "folder" },
  ];
  expect(remoteExpansionEntries({ entries })).toEqual(entries);
});

it.each([
  null,
  [],
  false,
  { ...batch, id: 0 },
  { ...batch, status: "unknown" },
  { ...batch, direction: "unknown" },
  { ...batch, archive_name: 1 },
  { ...batch, items: {} },
])("rejects malformed batch envelopes %j", (value) => expect(() => transferBatchResponse(value)).toThrow(/Invalid transfer batch/));

it.each([null, [], { id: 0, status: "pending" }, { id: 1, status: "unknown" }, { id: 1, status: "pending", file_name: 2 }])(
  "rejects malformed transfer items %j",
  (item) => expect(() => transferBatchResponse({ ...batch, items: [item] })).toThrow(/Invalid transfer batch/),
);

it.each([null, [], false, {}, { entries: null }])("rejects malformed expansion envelopes %j", (value) => {
  expect(() => remoteExpansionEntries(value)).toThrow(/Invalid remote folder response/);
});

it.each([
  null,
  [],
  { type: "unknown", path: "/a", name: "a" },
  { type: "file", path: 2, name: "a" },
  { type: "file", path: "a", name: "a" },
  { type: "file", path: "/a", name: 2 },
  { type: "file", path: "/a", name: "a", size: -1 },
  { type: "file", path: "/a", name: "a", size: 1.5 },
])("rejects malformed expansion entries %j", (entry) =>
  expect(() => remoteExpansionEntries({ entries: [entry] })).toThrow(/Invalid remote folder entry/),
);
