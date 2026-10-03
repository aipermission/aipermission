#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"
if [ -n "${DOCKER_CONTEXT:-}" ]; then
  endpoint=$(docker context inspect "$DOCKER_CONTEXT" --format '{{.Endpoints.docker.Host}}')
elif [ -n "${DOCKER_HOST:-}" ]; then
  endpoint=$DOCKER_HOST
else
  endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
fi
case "$endpoint" in
  unix:///*) ;;
  *) echo "Conformance requires a local Unix-socket Docker endpoint" >&2; exit 1 ;;
esac
stage=$(mktemp -d "${TMPDIR:-/tmp}/aipermission-conformance.XXXXXXXX")
project=$(basename "$stage" | tr '.A-Z' '-a-z')
fixture=backend/testdata/connector-conformance/compose.yml
export AIPERMISSION_CONFORMANCE_SOURCE="$stage/source"
started=0
dc() { docker compose --env-file /dev/null -p "$project" -f "$fixture" "$@"; }
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ "$started" = 1 ] && ! dc down -v --remove-orphans --rmi local; then status=1; fi
  rm -rf -- "$stage"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Archive the current candidate, including new code but excluding ignored
# operator files. Never mount the working repository, .env, or .git in a runner.
mkdir "$stage/source"
git ls-files -z --cached --others --exclude-standard -- \
  backend/go.mod backend/go.sum backend/cmd backend/internal \
  backend/testdata/connector-conformance/Dockerfile \
  scripts/verification-policy.json > "$stage/files"
tar -cf "$stage/source.tar" --null --verbatim-files-from -T "$stage/files"
tar -xf "$stage/source.tar" -C "$stage/source"

dc build runner
started=1
dc up -d --wait --wait-timeout 120 clickhouse postgres valkey rabbitmq minio kafka
dc run --rm --no-deps -T minio-init
if ! dc run --rm --no-deps -T runner; then
  dc logs --no-color --tail 100 kafka >&2 || true
  exit 1
fi
