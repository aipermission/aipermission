import { notifyChanged } from "./runtime.ts";

export function reportObservationFailure() {
  notifyChanged();
  console.warn("Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying.");
}
