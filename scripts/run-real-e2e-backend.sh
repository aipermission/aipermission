#!/usr/bin/env bash
set -euo pipefail

data_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$data_dir"
}
trap cleanup EXIT

export AIPERMISSION_BACKEND_HOST=127.0.0.1
export AIPERMISSION_BACKEND_PORT=18080
export AIPERMISSION_FRONTEND_PORT=4174
export AIPERMISSION_ALLOWED_ORIGINS=http://127.0.0.1:4174
export AIPERMISSION_DATA_PATH="$data_dir/aipermission.db"

cd "$(dirname "$0")/../backend"
if [[ -n "${AIPERMISSION_E2E_BINARY:-}" ]]; then
  "$AIPERMISSION_E2E_BINARY"
else
  go run -tags=e2e ./cmd/e2e
fi
