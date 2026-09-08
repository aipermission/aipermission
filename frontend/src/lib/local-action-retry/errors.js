export function ledgerFullError() {
  return new Error("The local action retry ledger is full. Reconcile unresolved requests in Settings before starting another action.");
}

export function storageError() {
  return new Error("Secure retry storage is unavailable; the connector action was not sent.");
}

export function retryIdentityChangedError() {
  return new Error("The protected retry identity changed in another tab. Refresh and reconcile the current entry before retrying.");
}
