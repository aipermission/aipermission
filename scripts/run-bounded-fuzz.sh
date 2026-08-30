#!/bin/sh
set -eu

duration=${AIPERMISSION_FUZZ_TIME:-1s}
case "$duration" in
  *ms) amount=${duration%ms} ;;
  *s) amount=${duration%s} ;;
  *)
    printf 'AIPERMISSION_FUZZ_TIME must use ms or s: %s\n' "$duration" >&2
    exit 2
    ;;
esac
case "$amount" in
  ''|*[!0-9]*)
    printf 'invalid AIPERMISSION_FUZZ_TIME: %s\n' "$duration" >&2
    exit 2
    ;;
esac
if [ "$amount" -lt 1 ]; then
  printf 'AIPERMISSION_FUZZ_TIME must be positive: %s\n' "$duration" >&2
  exit 2
fi
case "$duration" in
  *ms)
    if [ "$amount" -gt 30000 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must not exceed 30000ms: %s\n' "$duration" >&2
      exit 2
    fi
    ;;
  *s)
    if [ "$amount" -gt 30 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must not exceed 30s: %s\n' "$duration" >&2
      exit 2
    fi
    ;;
esac

run_fuzz() {
  package=$1
  target=$2
  printf '==> fuzz %s %s (%s)\n' "$package" "$target" "$duration"
  (cd backend && go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "$duration")
}

run_fuzz ./internal/api FuzzApprovalContextHash
run_fuzz ./internal/api FuzzBasicRedaction
run_fuzz ./internal/api FuzzTransferPathNormalization
run_fuzz ./internal/connectors/sqlsafe FuzzValidateReadOnly
run_fuzz ./internal/connectors/redis FuzzReadRESPValue
run_fuzz ./internal/backups FuzzValidateServiceMetadata
run_fuzz ./internal/connectors FuzzNormalizeSchemaValues
