import { cleanupMethods } from "./cleanup-types";
import type { CleanupChoice, CleanupEvidence, CleanupRecord, CleanupSnapshot, CleanupSubmission } from "./cleanup-types";
import { cleanupReason } from "./cleanup-identity";

export function cleanupSubmission(
  snapshot: CleanupSnapshot,
  record: CleanupRecord,
  choice: CleanupChoice,
  evidence: CleanupEvidence[],
  reason: string,
): CleanupSubmission {
  if (!snapshot.records.includes(record) || !record.choices.includes(choice))
    throw new Error("Reload SSH cleanup evidence before submitting.");
  if (!cleanupReason(reason)) throw new Error("Enter a decision reason of at most 2,000 UTF-8 bytes.");
  if (evidence.length !== choice.subjects.length) throw new Error("Verify every historical location independently.");
  const coverage = choice.subjects.map((subject, index) => {
    const proof = evidence[index];
    if (
      !proof ||
      proof.subject_id !== subject.id ||
      proof.absent !== true ||
      !cleanupMethods.includes(proof.method) ||
      !cleanupReason(proof.reason)
    )
      throw new Error("Confirm absence, an external method and an explanation for every location.");
    return { ...proof, reason: proof.reason.trim() };
  });
  return {
    resource_id: record.entry.resource_id,
    generation: record.entry.record.generation,
    deletion_context_digest: snapshot.deletion_context_digest,
    identity_digest: choice.digest,
    coverage,
    reason: reason.trim(),
  };
}
