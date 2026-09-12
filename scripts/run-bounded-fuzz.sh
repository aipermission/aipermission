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
	listed=$(cd backend && go test "$package" -run '^$' -list "^${target}$")
	if ! printf '%s\n' "$listed" | grep -qx "$target"; then
		printf 'fuzz target %s was not found in %s\n' "$target" "$package" >&2
		exit 1
	fi
  # A fixed execution budget and one worker avoid wall-clock shutdown races on
  # busy CI runners while still exercising generated input deterministically.
  (cd backend && go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "$budget" -parallel 1)
}

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
inventory=$(mktemp "${TMPDIR:-/tmp}/aipermission-fuzz-targets.XXXXXX")
trap 'rm -f "$inventory"' EXIT HUP INT TERM
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
