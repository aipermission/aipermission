import { localActionRetryObservationFailedEvent } from "./constants.ts";

export function reportObservationFailure() {
  if (typeof window !== "undefined" && typeof window.dispatchEvent === "function" && typeof CustomEvent === "function") {
    window.dispatchEvent(new CustomEvent(localActionRetryObservationFailedEvent));
  }
  console.warn("Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying.");
}
