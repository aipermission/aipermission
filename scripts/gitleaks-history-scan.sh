#!/bin/sh
set -eu

image="ghcr.io/gitleaks/gitleaks:v8.28.0@sha256:cdbb7c955abce02001a9f6c9f602fb195b7fadc1e812065883f695d1eeaba854"
repo_root="$(git rev-parse --show-toplevel)"

if ! history_count="$(docker run --rm \
  --network none \
  -v "$repo_root:/repo:ro" \
  --entrypoint git \
  "$image" \
  -c safe.directory=/repo \
  -C /repo \
  rev-list --all --count)"; then
  echo "gitleaks history preflight failed: mounted Git history is unreadable" >&2
  exit 1
fi
case "$history_count" in
  ''|*[!0-9]*)
    echo "gitleaks history preflight failed: mounted commit count is invalid" >&2
    exit 1
    ;;
esac
if [ "$history_count" -eq 0 ]; then
  echo "gitleaks history preflight failed: mounted repository contains no commits" >&2
  exit 1
fi

docker run --rm \
  --network none \
  -v "$repo_root:/repo:ro" \
  "$image" detect \
  --source=/repo \
  --redact \
  --log-opts=--all \
  --no-banner
