const dataQuery = /(?:\?|&)(?:raw|url)(?:&|$)/;
const workerQuery = /(?:\?|&)(?:worker|sharedworker)(?:&|$)/;

function executableImportIdentity(specifier) {
  // Vite 6's worker loader precedes its asset loader; both match the full ID.
  // Keep their literal flag matching before cleanUrl-style suffix removal.
  if (dataQuery.test(specifier) && !workerQuery.test(specifier)) return null;
  return specifier.split(/[?#]/, 1)[0];
}

module.exports = { executableImportIdentity };
