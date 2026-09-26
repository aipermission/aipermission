import { expect, it } from "vitest";
import { readS3Directories, readS3Lifecycle, readS3Metadata, readS3Objects, readS3Presign, readS3Versions, s3OutputRecord } from "./output";

it("normalizes only valid object and directory identities", () => {
  expect(s3OutputRecord(null)).toEqual({});
  expect(s3OutputRecord([])).toEqual({});
  expect(readS3Objects(null)).toEqual([]);
  expect(readS3Directories(false)).toEqual([]);
  expect(
    readS3Objects([null, { key: 1 }, { key: "file", size: [], etag: "x", last_modified: "today" }, { key: "other", size: "4" }]),
  ).toEqual([
    { key: "file", size: undefined, etag: "x", last_modified: "today" },
    { key: "other", size: "4", etag: undefined, last_modified: undefined },
  ]);
  expect(readS3Directories([{ prefix: "folder/", name: "folder" }, { prefix: 1 }])).toEqual([{ prefix: "folder/", name: "folder" }]);
});

it("retains raw metadata while preventing malformed fields from reaching renderers", () => {
  expect(readS3Metadata([])).toBeNull();
  expect(readS3Metadata({ key: "a", content_length: 4, headers: { valid: "x", malformed: [] }, custom: [1] })).toMatchObject({
    key: "a",
    content_length: 4,
    headers: { valid: "x" },
    custom: [1],
  });
  expect(readS3Metadata({ bucket: [], content_type: {}, content_length: false })).toMatchObject({
    bucket: undefined,
    content_type: undefined,
    content_length: undefined,
  });
});

it("requires a presigned URL and accepts only string headers", () => {
  expect(() => readS3Presign({ url: {} })).toThrow("Invalid S3 presigned URL response.");
  expect(
    readS3Presign({
      url: "https://example.test/object",
      operation: "upload",
      expires_at: "later",
      required_headers: { "Content-Type": "text/plain", invalid: 1 },
    }),
  ).toEqual({
    url: "https://example.test/object",
    operation: "upload",
    expires_at: "later",
    required_headers: { "Content-Type": "text/plain" },
  });
});

it("bounds version and lifecycle display data without inventing object identities", () => {
  for (const field of ["delete_marker", "is_latest"]) {
    expect(() => readS3Versions({ versions: [{ version_id: "v1", [field]: "true" }] })).toThrow("Invalid S3 object version flags.");
  }
  expect(readS3Versions(null)).toEqual({ versions: [], next_cursor: "" });
  expect(
    readS3Versions({
      versions: [null, { version_id: 3 }, { version_id: "v1", is_latest: true, delete_marker: false, size: 4, last_modified: "today" }],
      next_cursor: "next",
    }).versions,
  ).toEqual([{ version_id: "v1", is_latest: true, delete_marker: false, size: 4, last_modified: "today" }]);
  expect(
    readS3Lifecycle({
      configured: true,
      raw_xml: "xml",
      rules: [
        null,
        { id: 3 },
        {
          id: "rule",
          status: "Enabled",
          prefix: "logs/",
          expire_current_after_days: 4,
          expire_noncurrent_after_days: 7,
          abort_incomplete_multipart_days: 1,
        },
      ],
    }),
  ).toEqual({
    configured: true,
    raw_xml: "xml",
    rules: [
      {
        id: "rule",
        status: "Enabled",
        prefix: "logs/",
        expire_current_after_days: 4,
        expire_noncurrent_after_days: 7,
        abort_incomplete_multipart_days: 1,
      },
    ],
  });
  expect(readS3Lifecycle({ rules: [{ id: "empty" }] }).rules[0]).toEqual({
    id: "empty",
    status: "",
    prefix: "",
    expire_current_after_days: 0,
    expire_noncurrent_after_days: 0,
    abort_incomplete_multipart_days: 0,
  });
  expect(readS3Lifecycle(null)).toEqual({ configured: false, rules: [], raw_xml: "" });
});
