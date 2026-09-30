import { createHash } from "node:crypto";
import type { CleanupEvidence, CleanupSubmission } from "./cleanup-types";

export const cleanupTargetID = 7;
export const cleanupGeneration = "0123456789abcdef0123456789abcdef";
export const cleanupNextGeneration = "fedcba9876543210fedcba9876543210";
export const cleanupContextDigest = "0123456789abcdef".repeat(4);
export const cleanupHostFingerprint = `SHA256:${"A".repeat(43)}`;

function digest(value: unknown): string {
  return createHash("sha256").update(JSON.stringify(value)).digest("hex");
}

function identity(host: string, targetID: number) {
  return {
    target_id: targetID,
    target_revision: "target-public-revision-1",
    config_digest: "1".repeat(64),
    host,
    port: 22,
    username: "operator",
    key_digest: "0".repeat(64),
    host_fingerprints: [cleanupHostFingerprint],
    profiles: [
      {
        id: 11,
        revision: "profile-public-revision-1",
        secret_revision: "opaque-revision-1",
        public_digest: "2".repeat(64),
        key_id: 13,
        key_revision: "key-public-revision-1",
      },
    ],
  };
}

function subject(location: ReturnType<typeof identity>) {
  // Go hashes VerificationSubject with its ID still empty, in struct field order.
  const value = {
    id: "",
    host: location.host,
    port: location.port,
    username: location.username,
    key_fingerprint: cleanupHostFingerprint,
    host_fingerprints: [...location.host_fingerprints],
  };
  return { ...value, id: digest(value) };
}

export function cleanupFixture(targetID = cleanupTargetID, retired = false) {
  const historical = identity("retired.example.test", targetID);
  const current = identity("cleanup.example.test", targetID);
  const historicalSubject = subject(historical);
  const subjects = [historicalSubject, subject(current)].sort((a, b) => a.id.localeCompare(b.id));
  const coverage: CleanupEvidence[] = subjects.map((value, index) => ({
    subject_id: value.id,
    method: index === 0 ? "provider_console" : "independent_admin_session",
    absent: true,
    reason: `Verified ${value.host} through its external administration console.`,
  }));
  const historicalProof = {
    identity: structuredClone(historical),
    deletion_context_digest: "3".repeat(64),
    coverage: [{ ...coverage[subjects.findIndex((value) => value.id === historicalSubject.id)] }],
    reason: "Earlier external verification of the retired location.",
  };
  const currentChoice = { identity: current, digest: digest(current), subjects };
  const historicalChoice = { identity: historical, digest: digest(historical), subjects: [historicalSubject] };
  const selected = retired ? historicalChoice : currentChoice;
  const wire = {
    target_id: targetID,
    deletion_context_digest: cleanupContextDigest,
    records: [
      {
        entry: {
          resource_id: 41,
          record: { version: 2, identity: historical, generation: cleanupGeneration, status: "attested", attestations: [historicalProof] },
        },
        choices: [selected],
      },
    ],
  };
  const submission: CleanupSubmission = {
    resource_id: 41,
    generation: cleanupGeneration,
    deletion_context_digest: cleanupContextDigest,
    identity_digest: wire.records[0].choices[0].digest,
    coverage: structuredClone(coverage.filter((value) => selected.subjects.some((subject) => subject.id === value.subject_id))),
    reason: "All historical authorized-key locations were independently checked.",
  };
  const acknowledgement = {
    ok: true,
    entry: {
      resource_id: 41,
      record: {
        version: 2,
        identity: structuredClone(historical),
        generation: cleanupNextGeneration,
        status: "attested",
        attestations: [
          structuredClone(historicalProof),
          {
            identity: structuredClone(selected.identity),
            deletion_context_digest: submission.deletion_context_digest,
            coverage: structuredClone(submission.coverage),
            reason: submission.reason,
          },
        ],
      },
    },
  };
  return { wire, coverage: structuredClone(submission.coverage), submission, acknowledgement };
}
