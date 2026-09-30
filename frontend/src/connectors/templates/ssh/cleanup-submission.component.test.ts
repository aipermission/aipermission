import { describe, expect, it } from "vitest";
import { cleanupSnapshot } from "./cleanup-contracts";
import { cleanupSubmission } from "./cleanup-submission";
import { cleanupFixture, cleanupTargetID } from "./cleanup-test-fixtures";
import { cleanupMethods } from "./cleanup-types";
import type { CleanupEvidence } from "./cleanup-types";

function selection() {
  const fixture = cleanupFixture();
  const snapshot = cleanupSnapshot(fixture.wire, cleanupTargetID);
  const record = snapshot.records[0];
  const choice = record.choices[0];
  return { ...fixture, snapshot, record, choice };
}

describe("SSH cleanup submission ownership", () => {
  it("uses exact backend selection identities and ordered independent evidence", () => {
    const { snapshot, record, choice, coverage, submission } = selection();
    const actual = cleanupSubmission(snapshot, record, choice, coverage, submission.reason);
    expect(actual).toEqual(submission);
    expect(actual.coverage.map((proof) => proof.subject_id)).toEqual(choice.subjects.map((subject) => subject.id));
    expect(new Set(actual.coverage.map((proof) => proof.reason)).size).toBe(2);
  });

  it("rejects cloned records, cloned choices and objects from another snapshot", () => {
    const { snapshot, record, choice, coverage, submission, wire } = selection();
    const foreign = cleanupSnapshot(wire, cleanupTargetID).records[0];
    for (const [selectedRecord, selectedChoice] of [
      [structuredClone(record), choice],
      [record, structuredClone(choice)],
      [foreign, foreign.choices[0]],
      [record, foreign.choices[0]],
    ] as const)
      expect(() => cleanupSubmission(snapshot, selectedRecord, selectedChoice, coverage, submission.reason)).toThrow(/Reload/);
  });

  it("rejects a choice owned by another record in the same snapshot", () => {
    const { wire, coverage, submission } = cleanupFixture();
    const other = structuredClone(wire.records[0]);
    other.entry.resource_id = 42;
    wire.records.push(other);
    const snapshot = cleanupSnapshot(wire, cleanupTargetID);
    expect(() => cleanupSubmission(snapshot, snapshot.records[0], snapshot.records[1].choices[0], coverage, submission.reason)).toThrow(
      /Reload/,
    );
  });

  it("allows another exact in-snapshot choice with its own complete subject set", () => {
    const { snapshot, record, coverage, submission } = selection();
    const choice = cleanupSnapshot(cleanupFixture(cleanupTargetID, true).wire, cleanupTargetID).records[0].choices[0];
    record.choices.push(choice);
    const proof = coverage.find((value) => value.subject_id === choice.subjects[0].id)!;
    expect(cleanupSubmission(snapshot, record, choice, [proof], submission.reason)).toMatchObject({
      identity_digest: choice.digest,
      coverage: [proof],
    });
  });

  it("copies coverage so subsequent input mutation cannot rewrite the decision", () => {
    const { snapshot, record, choice, coverage, submission } = selection();
    const actual = cleanupSubmission(snapshot, record, choice, coverage, submission.reason);
    expect(actual.coverage).not.toBe(coverage);
    expect(actual.coverage[0]).not.toBe(coverage[0]);
    coverage[0].reason = "Rewritten";
    coverage[0].subject_id = "f".repeat(64);
    coverage[0].absent = false;
    coverage.reverse();
    coverage.pop();
    expect(actual).toEqual(submission);
  });
});

describe("SSH cleanup evidence requirements", () => {
  it("rejects missing, extra, duplicate, reordered and unknown-subject coverage", () => {
    const { snapshot, record, choice, coverage, submission } = selection();
    for (const proofs of [
      [],
      coverage.slice(0, 1),
      [...coverage, coverage[0]],
      [...coverage].reverse(),
      [coverage[0], coverage[0]],
      [{ ...coverage[0], subject_id: "f".repeat(64) }, coverage[1]],
    ])
      expect(() => cleanupSubmission(snapshot, record, choice, proofs, submission.reason)).toThrow();
  });

  it.each([0, 1])("independently validates location %i absence, method and reason", (index) => {
    const { snapshot, record, choice, coverage, submission } = selection();
    for (const change of [
      { absent: false },
      { method: "ssh_session" },
      { method: "" },
      { reason: " \t\n" },
      { reason: "a".repeat(2001) },
    ]) {
      const proofs = structuredClone(coverage);
      Object.assign(proofs[index], change);
      expect(() => cleanupSubmission(snapshot, record, choice, proofs, submission.reason)).toThrow();
    }
  });

  it.each(cleanupMethods)("accepts external method %s for every location", (method) => {
    const { snapshot, record, choice, coverage, submission } = selection();
    const proofs = coverage.map((proof) => ({ ...proof, method }));
    expect(cleanupSubmission(snapshot, record, choice, proofs, submission.reason).coverage).toEqual(proofs);
  });

  it.each([null, undefined, 0, "true", 1])("requires literal boolean absence, not %j", (absent) => {
    const { snapshot, record, choice, coverage, submission } = selection();
    const proofs = [{ ...coverage[0], absent }, coverage[1]] as CleanupEvidence[];
    expect(() => cleanupSubmission(snapshot, record, choice, proofs, submission.reason)).toThrow();
  });

  it("rejects null or missing evidence objects", () => {
    const { snapshot, record, choice, coverage, submission } = selection();
    for (const proof of [null, undefined])
      expect(() => cleanupSubmission(snapshot, record, choice, [proof, coverage[1]] as CleanupEvidence[], submission.reason)).toThrow();
  });
});

describe("SSH cleanup UTF-8 reason bounds", () => {
  it.each([
    ["ASCII", "a".repeat(2000)],
    ["two-byte", "\u00e9".repeat(1000)],
    ["four-byte", "\u{1f600}".repeat(500)],
  ])("accepts exactly 2,000 bytes of %s after trimming", (_label, reason) => {
    const { snapshot, record, choice, coverage } = selection();
    expect(new TextEncoder().encode(reason).length).toBe(2000);
    const padded = ` \t\n\u00a0${reason}\u00a0\n\t `;
    const proofs = coverage.map((proof) => ({ ...proof, reason: padded }));
    const actual = cleanupSubmission(snapshot, record, choice, proofs, padded);
    expect(actual.reason).toBe(reason);
    expect(actual.coverage.map((proof) => proof.reason)).toEqual([reason, reason]);
    expect(proofs[0].reason).toBe(padded);
  });

  it.each([
    ["ASCII", "a".repeat(2001)],
    ["two-byte", "\u00e9".repeat(1000) + "a"],
    ["four-byte", "\u{1f600}".repeat(500) + "a"],
  ])("rejects 2,001 bytes of %s in both decision and each location reason", (_label, reason) => {
    const { snapshot, record, choice, coverage, submission } = selection();
    expect(new TextEncoder().encode(reason).length).toBe(2001);
    expect(() => cleanupSubmission(snapshot, record, choice, coverage, ` \n${reason}\t `)).toThrow(/2,000 UTF-8 bytes/);
    for (const index of [0, 1]) {
      const proofs = structuredClone(coverage);
      proofs[index].reason = reason;
      expect(() => cleanupSubmission(snapshot, record, choice, proofs, submission.reason)).toThrow();
    }
  });

  it.each(["", " \t\n\u00a0"])("rejects blank decision and location reasons %j", (reason) => {
    const { snapshot, record, choice, coverage } = selection();
    expect(() => cleanupSubmission(snapshot, record, choice, coverage, reason)).toThrow();
    for (const index of [0, 1]) {
      const proofs = structuredClone(coverage);
      proofs[index].reason = reason;
      expect(() => cleanupSubmission(snapshot, record, choice, proofs, "Externally verified.")).toThrow();
    }
  });
});
