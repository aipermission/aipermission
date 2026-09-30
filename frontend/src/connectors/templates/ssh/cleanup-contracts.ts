import { objectRecord } from "../../../lib/api-types";
import type { CleanupChoice, CleanupEntry, CleanupRecord, CleanupSnapshot, CleanupSubject, CleanupSubmission } from "./cleanup-types";
import {
  cleanupIdentity,
  cleanupDigest as hash,
  cleanupText as text,
  cleanupFingerprint as fingerprint,
  cleanupPins,
  cleanupPort,
  positiveID,
  cleanupReason,
} from "./cleanup-identity";
import { cleanupMethods } from "./cleanup-types";

const generation = /^[a-f0-9]{32}$/;

export function cleanupSnapshot(value: unknown, targetID: number): CleanupSnapshot {
  const snapshot = objectRecord(value);
  if (
    !Number.isSafeInteger(targetID) ||
    targetID < 1 ||
    !snapshot ||
    snapshot.target_id !== targetID ||
    !hash(snapshot.deletion_context_digest) ||
    !Array.isArray(snapshot.records)
  )
    throw new Error("Invalid SSH cleanup status response.");
  const records = snapshot.records.map(cleanupRecord);
  if (new Set(records.map((record) => record.entry.resource_id)).size !== records.length) throw new Error("Duplicate SSH cleanup records.");
  return { target_id: targetID, deletion_context_digest: snapshot.deletion_context_digest, records };
}

function cleanupRecord(value: unknown): CleanupRecord {
  const record = objectRecord(value);
  if (!record || !Array.isArray(record.choices) || record.choices.length === 0) throw new Error("Invalid SSH cleanup choices.");
  const choices = record.choices.map(cleanupChoice);
  if (new Set(choices.map((choice) => choice.digest)).size !== choices.length) throw new Error("Duplicate SSH cleanup choices.");
  return { entry: cleanupEntry(record.entry), choices };
}

function cleanupEntry(value: unknown): CleanupEntry {
  const entry = objectRecord(value);
  const record = objectRecord(entry?.record);
  if (
    !entry ||
    !positiveID(entry.resource_id) ||
    !record ||
    record.version !== 2 ||
    typeof record.generation !== "string" ||
    !generation.test(record.generation) ||
    (record.status !== "intent" && record.status !== "confirmed" && record.status !== "attested")
  )
    throw new Error("Invalid SSH cleanup entry.");
  const attestations = record.attestations === undefined ? [] : record.attestations;
  if (!Array.isArray(attestations) || (record.status === "attested" ? attestations.length === 0 : attestations.length !== 0))
    throw new Error("Invalid SSH cleanup attestation history.");
  return {
    resource_id: entry.resource_id,
    record: {
      version: 2,
      identity: cleanupIdentity(record.identity),
      generation: record.generation,
      status: record.status,
      attestations: attestations.map(cleanupAttestation),
    },
  };
}

function cleanupAttestation(value: unknown) {
  const proof = objectRecord(value);
  if (
    !proof ||
    !hash(proof.deletion_context_digest) ||
    !cleanupReason(proof.reason) ||
    proof.reason !== proof.reason.trim() ||
    !Array.isArray(proof.coverage) ||
    proof.coverage.length === 0
  )
    throw new Error("Invalid SSH cleanup attestation history.");
  const coverage = proof.coverage.map((value) => {
    const item = objectRecord(value);
    const method = cleanupMethods.find((method) => method === item?.method);
    if (
      !item ||
      !hash(item.subject_id) ||
      item.absent !== true ||
      !method ||
      !cleanupReason(item.reason) ||
      item.reason !== item.reason.trim()
    )
      throw new Error("Invalid SSH cleanup historical evidence.");
    return { subject_id: item.subject_id, method, absent: true, reason: item.reason };
  });
  if (new Set(coverage.map((value) => value.subject_id)).size !== coverage.length)
    throw new Error("Duplicate SSH cleanup historical evidence.");
  return {
    identity: cleanupIdentity(proof.identity),
    deletion_context_digest: proof.deletion_context_digest,
    reason: proof.reason,
    coverage,
  };
}

function cleanupChoice(value: unknown): CleanupChoice {
  const choice = objectRecord(value);
  if (!choice || !hash(choice.digest) || !Array.isArray(choice.subjects) || choice.subjects.length === 0)
    throw new Error("Invalid SSH cleanup identity choice.");
  const subjects = choice.subjects.map(cleanupSubject);
  if (new Set(subjects.map((subject) => subject.id)).size !== subjects.length) throw new Error("Duplicate SSH cleanup subjects.");
  return { identity: cleanupIdentity(choice.identity), digest: choice.digest, subjects };
}

function cleanupSubject(value: unknown): CleanupSubject {
  const subject = objectRecord(value);
  if (
    !subject ||
    !hash(subject.id) ||
    !text(subject.host) ||
    !text(subject.username) ||
    !fingerprint(subject.key_fingerprint) ||
    !cleanupPort(subject.port) ||
    !cleanupPins(subject.host_fingerprints)
  )
    throw new Error("Invalid SSH cleanup verification subject.");
  return {
    id: subject.id,
    host: subject.host,
    port: subject.port,
    username: subject.username,
    key_fingerprint: subject.key_fingerprint,
    host_fingerprints: [...subject.host_fingerprints],
  };
}

export function assertCleanupAcknowledgement(value: unknown, submission: CleanupSubmission, choice: CleanupChoice): void {
  const response = objectRecord(value);
  const entry = cleanupEntry(response?.entry);
  const rawRecord = objectRecord(objectRecord(response?.entry)?.record);
  const proofs = rawRecord?.attestations;
  const proof = Array.isArray(proofs) ? objectRecord(proofs.at(-1)) : null;
  if (
    response?.ok !== true ||
    entry.resource_id !== submission.resource_id ||
    entry.record.status !== "attested" ||
    entry.record.generation === submission.generation ||
    choice.digest !== submission.identity_digest ||
    JSON.stringify(entry.record.attestations.at(-1)?.identity) !== JSON.stringify(choice.identity) ||
    proof?.deletion_context_digest !== submission.deletion_context_digest ||
    proof.reason !== submission.reason ||
    !Array.isArray(proof.coverage) ||
    proof.coverage.length !== submission.coverage.length ||
    !proof.coverage.every((value, index) => {
      const actual = objectRecord(value);
      const expected = submission.coverage[index];
      return (
        actual?.subject_id === expected.subject_id &&
        actual?.method === expected.method &&
        actual?.absent === true &&
        actual?.reason === expected.reason
      );
    })
  )
    throw new Error("SSH cleanup decision acknowledgement is invalid; inspect the recorded evidence before retrying.");
}
