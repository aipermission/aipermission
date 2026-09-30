export type CleanupStatus = "intent" | "confirmed" | "attested";
export type CleanupSubject = {
  id: string;
  host: string;
  port: number;
  username: string;
  key_fingerprint: string;
  host_fingerprints: string[];
};
export type CleanupIdentity = {
  target_id: number;
  target_revision: string;
  config_digest: string;
  host: string;
  port: number;
  username: string;
  key_digest: string;
  host_fingerprints: string[];
  profiles: { id: number; revision: string; secret_revision: string; public_digest: string; key_id: number; key_revision: string }[];
};
export type CleanupChoice = { identity: CleanupIdentity; digest: string; subjects: CleanupSubject[] };
export type CleanupAttestation = {
  identity: CleanupIdentity;
  deletion_context_digest: string;
  coverage: CleanupEvidence[];
  reason: string;
};
export type CleanupEntry = {
  resource_id: number;
  record: { version: 2; identity: CleanupIdentity; generation: string; status: CleanupStatus; attestations: CleanupAttestation[] };
};
export type CleanupRecord = { entry: CleanupEntry; choices: CleanupChoice[] };
export type CleanupSnapshot = { target_id: number; deletion_context_digest: string; records: CleanupRecord[] };
export const cleanupMethods = ["provider_console", "independent_admin_session", "decommissioned_location"] as const;
export type CleanupMethod = (typeof cleanupMethods)[number];
export type CleanupEvidence = { subject_id: string; method: CleanupMethod; absent: boolean; reason: string };
export type CleanupSubmission = {
  resource_id: number;
  generation: string;
  deletion_context_digest: string;
  identity_digest: string;
  coverage: CleanupEvidence[];
  reason: string;
};
