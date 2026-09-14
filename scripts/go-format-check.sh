#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/backend"

unformatted=$(find . -type f -name '*.go' -not -path './vendor/*' -exec gofmt -l {} +)
if [ -n "$unformatted" ]; then
	printf '%s\n' 'Go source files must be formatted with gofmt:' >&2
	printf '%s\n' "$unformatted" >&2
	exit 1
fi

printf '%s\n' 'Go formatting check passed.'
