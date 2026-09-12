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
    if [ "$amount" -lt 100 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must be at least 100ms: %s\n' "$budget" >&2
      exit 2
    fi
    ;;
  *s) ;;
  *x)
    if [ "$amount" -lt 100 ]; then
      printf 'AIPERMISSION_FUZZ_TIME must be at least 100x: %s\n' "$budget" >&2
      exit 2
    fi
    ;;
esac
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
	listed=$(cd backend && go test "$package" -run '^$' -list "^${target}$")
	if ! printf '%s\n' "$listed" | grep -qx "$target"; then
		printf 'fuzz target %s was not found in %s\n' "$target" "$package" >&2
		exit 1
	fi
  : >"$events"
  if ! (cd backend && go test -json "$package" -run '^$' -fuzz "^${target}$" -fuzztime "$budget" -parallel 1 >"$events"); then
    cat "$events"
    return 1
  fi
  cat "$events"
  if ! node "$root/scripts/verify-fuzz-events.js" "$events" "$target" "$budget"; then
    return 1
  fi
}

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
inventory=$(mktemp "${TMPDIR:-/tmp}/aipermission-fuzz-targets.XXXXXX")
events=$(mktemp "${TMPDIR:-/tmp}/aipermission-fuzz-events.XXXXXX")
trap 'rm -f "$inventory" "$events"' EXIT HUP INT TERM
if ! node "$root/scripts/verification-policy.js" --list fuzz_targets >"$inventory"; then
  printf 'failed to produce the bounded fuzz target inventory\n' >&2
  exit 1
fi
if [ ! -s "$inventory" ]; then
  printf 'bounded fuzz target inventory is empty\n' >&2
  exit 1
fi
(cd backend && go run ./cmd/verification-runner fuzz-inventory)
while IFS="$(printf '\t')" read -r package target; do
  run_fuzz "$package" "$target"
done <"$inventory"
