#!/bin/sh
set -eu

budget=${AIPERMISSION_FUZZ_TIME:-1000x}
case "$budget" in
  *ms) amount=${budget%ms} ;;
  *s) amount=${budget%s} ;;
  *x) amount=${budget%x} ;;
  *)
    printf 'AIPERMISSION_FUZZ_TIME must use x, ms, or s: %s\n' "$budget" >&2
    exit 2
    ;;
esac
case "$amount" in
  ''|*[!0-9]*)
    printf 'invalid AIPERMISSION_FUZZ_TIME: %s\n' "$budget" >&2
    exit 2
    ;;
esac
if [ "$amount" -lt 1 ]; then
  printf 'AIPERMISSION_FUZZ_TIME must be positive: %s\n' "$budget" >&2
  exit 2
fi
case "$budget" in
  *ms)
    if [ "$amount" -gt 30000 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must not exceed 30000ms: %s\n' "$budget" >&2
      exit 2
    fi
    ;;
  *s)
    if [ "$amount" -gt 30 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must not exceed 30s: %s\n' "$budget" >&2
      exit 2
    fi
    ;;
  *x)
    if [ "$amount" -gt 100000 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must not exceed 100000x: %s\n' "$budget" >&2
      exit 2
    fi
    ;;
esac

run_fuzz() {
  package=$1
  target=$2
  printf '==> fuzz %s %s (%s)\n' "$package" "$target" "$budget"
  # A fixed execution budget and one worker avoid wall-clock shutdown races on
  # busy CI runners while still exercising generated input deterministically.
  (cd backend && go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "$budget" -parallel 1)
}

run_fuzz ./internal/actions FuzzApprovalContextHash
run_fuzz ./internal/api FuzzBasicRedaction
run_fuzz ./internal/api FuzzTransferPathNormalization
run_fuzz ./internal/connectors/sqlsafe FuzzValidateReadOnly
run_fuzz ./internal/connectors/redis FuzzReadRESPValue
run_fuzz ./internal/backups FuzzValidateServiceMetadata
run_fuzz ./internal/connectors FuzzNormalizeSchemaValues
