import { describe, expect, it } from "vitest";
import { cleanupIdentity } from "./cleanup-identity";
import { assertCleanupAcknowledgement, cleanupSnapshot } from "./cleanup-contracts";
import { cleanupFixture, cleanupTargetID } from "./cleanup-test-fixtures";

describe("public cleanup identity ownership", () => {
  it("preserves the complete server identity independently of input object order and mutation", () => {
    const original = cleanupFixture().wire.records[0].choices[0].identity;
    const shuffled = Object.fromEntries(Object.entries(original).reverse());
    const actual = cleanupIdentity(shuffled);
    expect(actual).toEqual(original);
    expect(JSON.stringify(actual)).toBe(JSON.stringify(original));
    original.host_fingerprints[0] = "rewritten";
    original.profiles[0].revision = "rewritten";
    expect(actual.host_fingerprints[0]).not.toBe("rewritten");
    expect(actual.profiles[0].revision).not.toBe("rewritten");
  });

  it.each([
    ["target_id", 0],
    ["target_id", Number.MAX_SAFE_INTEGER + 1],
    ["target_id", "7"],
    ["target_revision", ""],
    ["target_revision", null],
    ["config_digest", "bad"],
    ["host", ""],
    ["port", 0],
    ["port", 65536],
    ["username", null],
    ["key_digest", "bad"],
    ["host_fingerprints", []],
    ["host_fingerprints", null],
    ["profiles", null],
    ["profiles", []],
    ["profiles", [null]],
  ])("rejects identity %s = %j", (field, value) => {
    const identity = cleanupFixture().wire.records[0].choices[0].identity;
    expect(() => cleanupIdentity({ ...identity, [field as string]: value })).toThrow();
  });

  it.each([
    ["id", 0],
    ["id", "11"],
    ["key_id", 0],
    ["revision", ""],
    ["secret_revision", ""],
    ["public_digest", "bad"],
    ["key_revision", ""],
  ])("rejects profile %s = %j", (field, value) => {
    const identity = cleanupFixture().wire.records[0].choices[0].identity;
    expect(() => cleanupIdentity({ ...identity, profiles: [{ ...identity.profiles[0], [field as string]: value }] })).toThrow();
  });

  it("rejects missing or unknown fields and duplicate/out-of-order profiles", () => {
    const original = cleanupFixture().wire.records[0].choices[0].identity;
    for (const field of Object.keys(original)) {
      const identity = { ...original };
      Reflect.deleteProperty(identity, field);
      expect(() => cleanupIdentity(identity)).toThrow();
    }
    expect(() => cleanupIdentity({ ...original, unexpected_binding: "unmodeled" })).toThrow();
    expect(() => cleanupIdentity({ ...original, profiles: [{ ...original.profiles[0], unknown: "field" }] })).toThrow();
    const profile = original.profiles[0];
    expect(() => cleanupIdentity({ ...original, profiles: [profile, { ...profile }] })).toThrow();
    expect(() => cleanupIdentity({ ...original, profiles: [{ ...profile, id: 12 }, profile] })).toThrow();
    expect(cleanupIdentity({ ...original, profiles: [profile, { ...profile, id: 12 }] }).profiles).toHaveLength(2);
  });
});

describe("complete cleanup decision receipts", () => {
  it.each(["target_revision", "config_digest", "host", "username", "key_digest"])(
    "rejects acknowledgement changing identity %s even with matching coverage",
    (field) => {
      const { wire, acknowledgement, submission } = cleanupFixture();
      const selected = cleanupSnapshot(wire, cleanupTargetID).records[0].choices[0];
      const last = acknowledgement.entry.record.attestations.at(-1)!;
      const original = last.identity[field as keyof typeof last.identity];
      Object.assign(last.identity, { [field]: String(original).replace(/^./, "f") });
      expect(() => assertCleanupAcknowledgement(acknowledgement, submission, selected)).toThrow();
    },
  );

  it("rejects missing identity, changed profile revision, trust or selected digest", () => {
    const { wire, acknowledgement, submission } = cleanupFixture();
    const selected = cleanupSnapshot(wire, cleanupTargetID).records[0].choices[0];
    const proof = acknowledgement.entry.record.attestations.at(-1)!;
    for (const identity of [
      undefined,
      { ...proof.identity, profiles: [{ ...proof.identity.profiles[0], revision: "changed" }] },
      { ...proof.identity, host_fingerprints: [`SHA256:${"E".repeat(42)}A`] },
    ]) {
      const changed = structuredClone(acknowledgement);
      Object.assign(changed.entry.record.attestations.at(-1)!, { identity });
      expect(() => assertCleanupAcknowledgement(changed, submission, selected)).toThrow();
    }
    expect(() => assertCleanupAcknowledgement(acknowledgement, submission, { ...selected, digest: "f".repeat(64) })).toThrow();
    expect(() => assertCleanupAcknowledgement(acknowledgement, submission, selected)).not.toThrow();
  });

  it("accepts a differently ordered JSON identity without dropping historical decisions", () => {
    const { wire, acknowledgement, submission } = cleanupFixture();
    const selected = cleanupSnapshot(wire, cleanupTargetID).records[0].choices[0];
    const proof = acknowledgement.entry.record.attestations.at(-1)!;
    Object.assign(proof, { identity: Object.fromEntries(Object.entries(proof.identity).reverse()) });
    expect(() => assertCleanupAcknowledgement(acknowledgement, submission, selected)).not.toThrow();
    const snapshot = cleanupSnapshot({ ...wire, records: [{ ...wire.records[0], entry: acknowledgement.entry }] }, cleanupTargetID);
    expect(snapshot.records[0].entry.record.attestations).toEqual(acknowledgement.entry.record.attestations);
  });

  it.each([null, [], [null], [{ reason: "unknown" }]])("rejects invalid attested history %j", (attestations) => {
    const { wire } = cleanupFixture();
    Object.assign(wire.records[0].entry.record, { attestations });
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it("rejects malformed historical evidence instead of silently hiding it", () => {
    const { wire } = cleanupFixture();
    const proof = wire.records[0].entry.record.attestations[0];
    for (const change of [
      { deletion_context_digest: "bad" },
      { reason: " padded " },
      { reason: "a".repeat(2001) },
      { coverage: [] },
      { coverage: [proof.coverage[0], proof.coverage[0]] },
    ]) {
      const changed = structuredClone(wire);
      Object.assign(changed.records[0].entry.record.attestations[0], change);
      expect(() => cleanupSnapshot(changed, cleanupTargetID)).toThrow();
    }
    for (const change of [
      { subject_id: "bad" },
      { method: "ssh_session" },
      { absent: "true" },
      { reason: null },
      { reason: "\u00e9".repeat(1001) },
      { reason: " padded " },
    ]) {
      const changed = structuredClone(wire);
      Object.assign(changed.records[0].entry.record.attestations[0].coverage[0], change);
      expect(() => cleanupSnapshot(changed, cleanupTargetID)).toThrow();
    }
  });
});
