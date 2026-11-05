#!/bin/bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/backend"
export GOWORK=off
stage=$(mktemp -d "${TMPDIR:-/tmp}/aipermission-vuln.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
cp go.mod "$stage/go.mod"
cp go.sum "$stage/go.sum"
go mod edit -modfile="$stage/go.mod" -dropreplace=github.com/microsoft/go-mssqldb
scan() {
  if [ "$#" -gt 0 ]; then "$1" ./...
  else go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
  fi
}
status=0
scan "$@" || status=1
# Local replacement paths have no upstream advisory identity. Independently
# scan the original pinned dependency graph, without executing application code.
GOFLAGS="${GOFLAGS:-} -modfile=$stage/go.mod -mod=readonly" scan "$@" || status=1
exit "$status"
