export const localActionReconciliationEvent = "aipermission:local-action-reconciliation-required";
export const localActionRetryLedgerChangedEvent = "aipermission:local-action-retry-ledger-changed";

export const legacyStoragePrefix = "aipermission.local-action-retry.v2.";
export const databaseName = "aipermission-local-action-retry";
export const databaseVersion = 3;
export const entriesStore = "entries";
export const keysStore = "keys";
export const reservationsStore = "reservations";
export const attemptsStore = "attempts";
export const workspaceCookieName = "aipermission_workspace";
export const maxEntries = 128;
export const maxGlobalEntries = 512;
export const maxRetryScopes = 64;
export const signingReservationLifetimeMs = 2 * 60 * 1000;
export const actionAttemptLifetimeMs = 24 * 60 * 60 * 1000;
export const maxActionAttempts = 1024;
