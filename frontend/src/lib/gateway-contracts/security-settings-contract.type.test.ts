import type { SecuritySettingsUpdate } from "./security-settings-contract";

type Update = SecuritySettingsUpdate;
const common = { reusable_tokens: true, expose_mcp_server_metadata: false, mcp_start_enabled: true, redaction_mode: "basic" as const };
const expectedRevision: Update = { ...common, expected_revision: "r1" };
const revisionAlias: Update = { ...common, revision: "r1" };
const both: Update = { ...common, expected_revision: "r1", revision: "r1" };
const unknownAccepted: unknown extends Update ? true : false = false;
// @ts-expect-error An optimistic-concurrency revision is required.
const missingRevision: Update = common;
// @ts-expect-error Settings fields remain required in either revision branch.
const missingSettings: Update = { expected_revision: "r1" };
// @ts-expect-error Settings booleans cannot be substituted with text.
const malformedBoolean: Update = { ...common, reusable_tokens: "true", expected_revision: "r1" };
// @ts-expect-error Redaction mode is a closed wire enum.
const malformedMode: Update = { ...common, redaction_mode: "unknown", expected_revision: "r1" };
// @ts-expect-error Both revision field names accept only strings.
const malformedRevision: Update = { ...common, revision: 1 };
void expectedRevision;
void revisionAlias;
void both;
void unknownAccepted;
void missingRevision;
void missingSettings;
void malformedBoolean;
void malformedMode;
void malformedRevision;
