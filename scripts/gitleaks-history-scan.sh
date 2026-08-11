#!/bin/sh
set -eu

image="ghcr.io/gitleaks/gitleaks:v8.28.0@sha256:cdbb7c955abce02001a9f6c9f602fb195b7fadc1e812065883f695d1eeaba854"

docker run --rm \
  -v "$(pwd):/repo" \
  "$image" detect \
  --source=/repo \
  --redact \
  --log-opts=--all \
  --no-banner
