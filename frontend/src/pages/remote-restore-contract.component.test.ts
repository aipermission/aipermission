import { expect, it } from "vitest";
import { remoteBackupStreams, remoteBackupVersions } from "./remote-restore-contract.ts";

it("validates stream and immutable version identities", () => {
  expect(remoteBackupStreams({ items: [{ id: "stream-1", database_name: "Default" }] })).toEqual([{ id: "stream-1", database_name: "Default" }]);
  const version = { id: "backup-1", filename: "db.aipdb", created_at: "2026-09-01T00:00:00Z", size_bytes: 100, source_installation_id: "source" };
  expect(remoteBackupVersions({ items: [{ backups: [version] }] })).toEqual([version]);
  expect(remoteBackupVersions({ items: [] })).toEqual([]);
  expect(remoteBackupVersions({ items: [{ backups: [] }] })).toEqual([]);
});

it.each([null, {}, { items: null }, { items: [{}] }, { items: [{ id: 1, database_name: "Default" }] }, { items: [{ id: "s", database_name: {} }] }])(
  "rejects malformed streams: %j", (response) => expect(() => remoteBackupStreams(response)).toThrow(/Invalid backup/),
);

it.each([null, {}, { items: null }, { items: [{}] }, { items: [{ backups: [null] }] },
  { items: [{ backups: [{ id: "" }] }] }, { items: [{ backups: [{ id: "b", filename: {} }] }] },
  { items: [{ backups: [{ id: "b", size_bytes: -1 }] }] }, { items: [{ backups: [{ id: "b", size_bytes: "10" }] }] }])(
  "rejects malformed versions: %j", (response) => expect(() => remoteBackupVersions(response)).toThrow(/Invalid remote backup/),
);
