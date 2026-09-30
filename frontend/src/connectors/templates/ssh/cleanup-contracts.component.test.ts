import { describe, expect, it } from "vitest";
import { assertCleanupAcknowledgement as validateAcknowledgement, cleanupSnapshot } from "./cleanup-contracts";
import type { CleanupSubmission } from "./cleanup-types";
import { cleanupFixture, cleanupHostFingerprint, cleanupTargetID } from "./cleanup-test-fixtures";

function assertCleanupAcknowledgement(value: unknown, submission: CleanupSubmission) {
  validateAcknowledgement(value, submission, cleanupSnapshot(cleanupFixture().wire, cleanupTargetID).records[0].choices[0]);
}

describe("SSH cleanup snapshot contract", () => {
  it("projects public wire fields without deriving or replacing backend identity", () => {
    const { wire } = cleanupFixture();
    const snapshot = cleanupSnapshot(wire, cleanupTargetID);
    const choice = snapshot.records[0].choices[0];
    expect(snapshot).toEqual({
      target_id: wire.target_id,
      deletion_context_digest: wire.deletion_context_digest,
      records: [
        {
          entry: wire.records[0].entry,
          choices: wire.records[0].choices,
        },
      ],
    });
    expect(choice.subjects).toHaveLength(2);
    expect(new Set(choice.subjects.map((value) => value.host)).size).toBe(2);
    wire.records[0].choices[0].digest = "f".repeat(64);
    wire.records[0].choices[0].subjects[0].id = "e".repeat(64);
    expect(cleanupSnapshot(wire, cleanupTargetID).records[0].choices[0]).toMatchObject({
      digest: "f".repeat(64),
      subjects: [{ id: "e".repeat(64) }, {}],
    });
  });

  it.each(["intent", "confirmed", "attested"])("accepts wire status %s", (status) => {
    const { wire, acknowledgement } = cleanupFixture();
    const entry = status === "attested" ? acknowledgement.entry : wire.records[0].entry;
    expect(
      cleanupSnapshot(
        {
          ...wire,
          records: [
            {
              ...wire.records[0],
              entry: {
                ...entry,
                record: { ...entry.record, status, attestations: status === "attested" ? entry.record.attestations : [] },
              },
            },
          ],
        },
        cleanupTargetID,
      ).records[0].entry.record.status,
    ).toBe(status);
  });

  it("accepts an empty record set", () => {
    const { wire } = cleanupFixture();
    expect(cleanupSnapshot({ ...wire, records: [] }, cleanupTargetID).records).toEqual([]);
  });

  it("owns host pins independently of the response object", () => {
    const { wire } = cleanupFixture();
    const snapshot = cleanupSnapshot(wire, cleanupTargetID);
    wire.records[0].choices[0].subjects[0].host_fingerprints[0] = "rewritten";
    expect(snapshot.records[0].choices[0].subjects[0].host_fingerprints).toEqual([cleanupHostFingerprint]);
  });

  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1])("rejects invalid expected target %j", (targetID) => {
    const { wire } = cleanupFixture();
    expect(() => cleanupSnapshot({ ...wire, target_id: targetID }, targetID)).toThrow();
  });

  it.each([null, [], {}, { records: null }])("rejects malformed envelope %j", (value) => {
    expect(() => cleanupSnapshot(value, cleanupTargetID)).toThrow();
  });

  it.each([
    ["target_id", 8],
    ["target_id", "7"],
    ["target_id", null],
    ["deletion_context_digest", "A".repeat(64)],
    ["deletion_context_digest", "a".repeat(63)],
    ["deletion_context_digest", null],
    ["records", null],
    ["records", {}],
    ["records", [null]],
  ])("rejects envelope field %s = %j", (field, value) => {
    const { wire } = cleanupFixture();
    expect(() => cleanupSnapshot({ ...wire, [field as string]: value }, cleanupTargetID)).toThrow();
  });

  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, "41", null])("rejects resource ID %j", (resource_id) => {
    const { wire } = cleanupFixture();
    wire.records[0].entry = { ...wire.records[0].entry, resource_id } as (typeof wire.records)[0]["entry"];
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it.each([
    ["version", 1],
    ["version", "2"],
    ["version", null],
    ["generation", "a".repeat(31)],
    ["generation", "A".repeat(32)],
    ["generation", null],
    ["status", "complete"],
    ["status", null],
  ])("rejects record field %s = %j", (field, value) => {
    const { wire } = cleanupFixture();
    Object.assign(wire.records[0].entry.record, { [field as string]: value });
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it("rejects null entries and records, malformed choices, and duplicate identities", () => {
    const { wire } = cleanupFixture();
    const record = wire.records[0];
    for (const records of [
      [{ ...record, entry: null }],
      [{ ...record, entry: { resource_id: 41, record: null } }],
      [{ ...record, choices: null }],
      [{ ...record, choices: [] }],
      [{ ...record, choices: [null] }],
      [record, structuredClone(record)],
      [{ ...record, choices: [record.choices[0], structuredClone(record.choices[0])] }],
    ])
      expect(() => cleanupSnapshot({ ...wire, records }, cleanupTargetID)).toThrow();
  });

  it.each([
    ["digest", "A".repeat(64)],
    ["digest", "a".repeat(65)],
    ["digest", null],
    ["subjects", null],
    ["subjects", []],
    ["subjects", [null]],
    ["subjects", ["unknown"]],
    ["subjects", [{ host: "legacy.example.test", username: "operator", port: 22 }]],
  ])("rejects choice field %s = %j", (field, value) => {
    const { wire } = cleanupFixture();
    Object.assign(wire.records[0].choices[0], { [field as string]: value });
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it.each([
    ["id", "A".repeat(64)],
    ["id", "a".repeat(63)],
    ["id", null],
    ["host", " \t"],
    ["host", {}],
    ["username", ""],
    ["username", null],
    ["port", 0],
    ["port", 65536],
    ["port", 22.5],
    ["port", "22"],
    ["port", null],
    ["key_fingerprint", "MD5:00"],
    ["key_fingerprint", `${cleanupHostFingerprint}=`],
    ["key_fingerprint", `SHA256:${"A".repeat(42)}`],
    ["key_fingerprint", null],
    ["host_fingerprints", null],
    ["host_fingerprints", ["unknown"]],
  ])("rejects subject field %s = %j", (field, value) => {
    const { wire } = cleanupFixture();
    Object.assign(wire.records[0].choices[0].subjects[0], { [field as string]: value });
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it.each([1, 65535])("accepts boundary port %i", (port) => {
    const { wire } = cleanupFixture();
    wire.records[0].choices[0].subjects[0].port = port;
    expect(cleanupSnapshot(wire, cleanupTargetID).records[0].choices[0].subjects[0].port).toBe(port);
  });

  it("rejects repeated subject IDs even when locations differ", () => {
    const { wire } = cleanupFixture();
    wire.records[0].choices[0].subjects[1].id = wire.records[0].choices[0].subjects[0].id;
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it.each([{ pins: [] }, { pins: [cleanupHostFingerprint, cleanupHostFingerprint] }])(
    "rejects noncanonical host-pin set $pins",
    ({ pins }) => {
      const { wire } = cleanupFixture();
      wire.records[0].choices[0].subjects[0].host_fingerprints = pins;
      expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
    },
  );

  it.each(["key_fingerprint", "host_fingerprints"])("rejects noncanonical base64 in %s", (field) => {
    const { wire } = cleanupFixture();
    const invalid = `SHA256:${"A".repeat(42)}B`;
    Object.assign(wire.records[0].choices[0].subjects[0], { [field]: field === "key_fingerprint" ? invalid : [invalid] });
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });

  it("requires host pins in canonical order without silently rewriting evidence", () => {
    const { wire } = cleanupFixture();
    const subject = wire.records[0].choices[0].subjects[0];
    const later = `SHA256:${"E".repeat(42)}A`;
    subject.host_fingerprints = [cleanupHostFingerprint, later];
    expect(cleanupSnapshot(wire, cleanupTargetID).records[0].choices[0].subjects[0].host_fingerprints).toEqual(subject.host_fingerprints);
    subject.host_fingerprints.reverse();
    expect(() => cleanupSnapshot(wire, cleanupTargetID)).toThrow();
  });
});

describe("SSH cleanup acknowledgement contract", () => {
  it("accepts the exact latest full decision, not an older historical proof", () => {
    const { acknowledgement, submission } = cleanupFixture();
    expect(() => assertCleanupAcknowledgement(acknowledgement, submission)).not.toThrow();
    acknowledgement.entry.record.attestations.reverse();
    expect(() => assertCleanupAcknowledgement(acknowledgement, submission)).toThrow();
  });

  it("rejects malformed envelopes, false success, and absent latest attestations", () => {
    const { acknowledgement, submission } = cleanupFixture();
    for (const value of [null, {}, { ok: true, entry: null }, { ...acknowledgement, ok: false }, { ...acknowledgement, ok: "true" }])
      expect(() => assertCleanupAcknowledgement(value, submission)).toThrow();
    for (const attestations of [undefined, null, [], [null], [{ reason: submission.reason }]])
      expect(() =>
        assertCleanupAcknowledgement(
          {
            ...acknowledgement,
            entry: {
              ...acknowledgement.entry,
              record: { ...acknowledgement.entry.record, attestations },
            },
          },
          submission,
        ),
      ).toThrow();
  });

  it("rejects another resource, unchanged or malformed generation, version and status", () => {
    const { acknowledgement, submission } = cleanupFixture();
    expect(() =>
      assertCleanupAcknowledgement({ ...acknowledgement, entry: { ...acknowledgement.entry, resource_id: 42 } }, submission),
    ).toThrow();
    for (const change of [
      { generation: submission.generation },
      { generation: "A".repeat(32) },
      { generation: null },
      { version: 1 },
      { status: "confirmed" },
      { status: "intent" },
      { status: "unknown" },
    ])
      expect(() =>
        assertCleanupAcknowledgement(
          {
            ...acknowledgement,
            entry: {
              ...acknowledgement.entry,
              record: { ...acknowledgement.entry.record, ...change },
            },
          },
          submission,
        ),
      ).toThrow();
  });

  it.each([
    ["deletion_context_digest", "f".repeat(64)],
    ["deletion_context_digest", null],
    ["reason", "Another decision"],
    ["reason", null],
    ["coverage", null],
    ["coverage", []],
  ])("rejects latest decision field %s = %j", (field, value) => {
    const { acknowledgement, submission } = cleanupFixture();
    Object.assign(acknowledgement.entry.record.attestations.at(-1)!, { [field as string]: value });
    expect(() => assertCleanupAcknowledgement(acknowledgement, submission)).toThrow();
  });

  it("requires complete ordered coverage, including every evidence field", () => {
    const { acknowledgement, submission } = cleanupFixture();
    const proof = acknowledgement.entry.record.attestations.at(-1)!;
    const coverage = proof.coverage;
    for (const changed of [
      coverage.slice(0, 1),
      [...coverage, coverage[0]],
      [...coverage].reverse(),
      [coverage[0], coverage[0]],
      [null, coverage[1]],
      ...[
        { subject_id: "f".repeat(64) },
        { method: "decommissioned_location" },
        { absent: false },
        { absent: "true" },
        { reason: "Different verification" },
      ].map((change) => [{ ...coverage[0], ...change }, coverage[1]]),
    ])
      expect(() =>
        assertCleanupAcknowledgement(
          {
            ...acknowledgement,
            entry: {
              ...acknowledgement.entry,
              record: { ...acknowledgement.entry.record, attestations: [{ ...proof, coverage: changed }] },
            },
          },
          submission,
        ),
      ).toThrow();
  });
});
