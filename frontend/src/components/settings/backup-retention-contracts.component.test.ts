import { describe, expect, it } from "vitest";
import { backupStorageResponse, backupRetentionPolicyResponse, backupRetentionPreviewResponse, backupRetentionUpdateResponse } from "./backup-retention-contracts";

const storage = { used_bytes: 256, quota_enabled: false, pending_deletions: 0 };
const policy = { enabled: true, keep_latest: 10 };
const preview = { keep_latest: 10, retain_count: 3, retain_bytes: 256, delete_count: 0, delete_bytes: 0 };

describe("backup retention response boundaries", () => {
  it("accepts omitted quota fields when storage is unlimited", () => {
    expect(backupStorageResponse(storage)).toBe(storage);
  });

  it("requires numeric quota metadata when the quota is enabled", () => {
    expect(() => backupStorageResponse({ ...storage, quota_enabled: true })).toThrow("Backup storage quota response is invalid.");
    expect(backupStorageResponse({ ...storage, quota_enabled: true, quota_bytes: 1024, remaining_bytes: 768 }).remaining_bytes).toBe(768);
  });

  it("preserves backend-valid int64 byte totals beyond the exact JavaScript integer range", () => {
    const bytes = 2 ** 53;
    expect(backupStorageResponse({ ...storage, used_bytes: bytes, quota_enabled: true, quota_bytes: bytes, remaining_bytes: 0 }).used_bytes).toBe(bytes);
    expect(backupRetentionPreviewResponse({ ...preview, retain_bytes: bytes }).retain_bytes).toBe(bytes);
  });

  it.each([null, [], { ...storage, used_bytes: -1 }, { ...storage, pending_deletions: 0.5 }, { ...storage, quota_enabled: "false" }])("rejects malformed storage %j", (value) => {
    expect(() => backupStorageResponse(value)).toThrow();
  });

  it("accepts disabled policy without an omitted keep_latest field", () => {
    expect(backupRetentionPolicyResponse({ enabled: false })).toEqual({ enabled: false });
    expect(backupRetentionPolicyResponse(policy)).toBe(policy);
  });

  it.each([null, {}, { enabled: true }, { ...policy, keep_latest: 0 }, { ...policy, keep_latest: 1001 }, { enabled: "true", keep_latest: 10 }])("rejects malformed policy %j", (value) => {
    expect(() => backupRetentionPolicyResponse(value)).toThrow();
  });

  it("accepts preview counts and an update without a preview", () => {
    expect(backupRetentionPreviewResponse(preview)).toBe(preview);
    expect(backupRetentionUpdateResponse({ policy, deleted_count: 0 })).toEqual({ policy, deleted_count: 0, preview: null });
    expect(backupRetentionUpdateResponse({ policy, deleted_count: 0, preview }).preview).toBe(preview);
  });

  it.each([null, {}, { ...preview, delete_count: -1 }, { ...preview, retain_bytes: "256" }, { ...preview, keep_latest: 1001 }])("rejects malformed preview %j", (value) => {
    expect(() => backupRetentionPreviewResponse(value)).toThrow();
  });

  it.each([{}, { policy, deleted_count: -1 }, { policy: {}, deleted_count: 0 }, { policy, deleted_count: 0, preview: {} }])("rejects malformed updates %j", (value) => {
    expect(() => backupRetentionUpdateResponse(value)).toThrow();
  });
});
