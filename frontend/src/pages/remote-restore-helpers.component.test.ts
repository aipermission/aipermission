import { describe, expect, it } from "vitest";
import { groupBackupVersions, remoteCredentialFingerprint, remoteRequestIsCurrent } from "./remote-restore-helpers";

describe("remote restore helpers", () => {
  it("invalidates a request when the credentials or generation change", () => {
    const form = { current: { base_url: "https://backup.example", token: "first" } };
    const generation = { current: 2 };
    const fingerprint = remoteCredentialFingerprint(form.current);

    expect(remoteRequestIsCurrent(generation, 2, form, fingerprint)).toBe(true);
    form.current.token = "second";
    expect(remoteRequestIsCurrent(generation, 2, form, fingerprint)).toBe(false);
    expect(remoteRequestIsCurrent(generation, 1, form, remoteCredentialFingerprint(form.current))).toBe(false);
  });

  it("groups versions by source without reordering them", () => {
    const versions = [
      { id: "a", source_installation_id: "first" },
      { id: "b", source_installation_id: "second" },
      { id: "c", source_installation_id: "first" },
    ];

    expect(groupBackupVersions(versions)).toEqual([
      { source: "first", items: [versions[0], versions[2]] },
      { source: "second", items: [versions[1]] },
    ]);
  });
});
