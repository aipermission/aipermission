import { expect, it } from "vitest";
import { backupCountResponse, uploadedBackupRecordResponse } from "./backup-contracts";

it("accepts only a recorded upload with a visible filename", () => {
  expect(uploadedBackupRecordResponse({ id: 3, filename: "backup.aipdb" })).toEqual({ id: 3, filename: "backup.aipdb" });
  for (const value of [null, {}, { id: 3 }, { id: 3, filename: "" }, { id: 3, filename: {} }])
    expect(() => uploadedBackupRecordResponse(value)).toThrow();
});

it("validates deletion and retention counts before producing success feedback", () => {
  expect(backupCountResponse({ deleted_count: 2, keep_latest: 10 })).toEqual({ deleted_count: 2, keep_latest: 10 });
  expect(backupCountResponse({ deleted_count: 2 })).toEqual({ deleted_count: 2, keep_latest: 0 });
  for (const value of [
    null,
    {},
    { deleted_count: -1 },
    { deleted_count: 0.5 },
    { deleted_count: 2, keep_latest: "10" },
    { deleted_count: 2, keep_latest: -1 },
  ])
    expect(() => backupCountResponse(value)).toThrow();
});
