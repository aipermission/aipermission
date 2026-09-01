#!/usr/bin/env bash
set -euo pipefail

backend="ghcr.io/aipermission/aipermission-backend"
frontend="ghcr.io/aipermission/aipermission-frontend"
version="${GITHUB_REF_NAME#v}"
stable=0
[[ "${version}" != *-* ]] && stable=1

inspect_digest() {
  local output digest
  output="$(docker buildx imagetools inspect "$1")"
  digest="$(awk '/^Digest:/ {print $2; exit}' <<< "${output}")"
  test -n "${digest}"
  printf '%s' "${digest}"
}

resolve_optional_digest() {
  local reference="$1" output status
  set +e
  output="$(docker buildx imagetools inspect "${reference}" 2>&1)"
  status=$?
  set -e
  if [[ ${status} -eq 0 ]]; then
    local digest
    digest="$(awk '/^Digest:/ {print $2; exit}' <<< "${output}")"
    test -n "${digest}"
    printf '%s' "${digest}"
    return
  fi
  if [[ "${output}" == *"not found"* || "${output}" == *"manifest unknown"* || "${output}" == *"name unknown"* ]]; then
    return
  fi
  echo "Could not inspect ${reference}: ${output}" >&2
  return 1
}

promote() {
  local target="$1" source="$2" expected="$3" promoted
  docker buildx imagetools create --prefer-index=false --tag "${target}" "${source}@${expected}" || return
  promoted="$(inspect_digest "${target}")" || return
  test "${promoted}" = "${expected}"
}

assert_immutable_compatible() {
  local target="$1" expected="$2" existing
  existing="$(resolve_optional_digest "${target}")" || return
  if [[ -n "${existing}" && "${existing}" != "${expected}" ]]; then
    echo "Refusing to replace existing immutable image tag ${target} (${existing}); expected ${expected}." >&2
    return 1
  fi
}

ensure_immutable_tag() {
  local target="$1" source="$2" expected="$3" existing
  existing="$(resolve_optional_digest "${target}")" || return
  if [[ -z "${existing}" ]]; then
    promote "${target}" "${source}" "${expected}" || return
    return
  fi
  if [[ "${existing}" != "${expected}" ]]; then
    echo "Immutable image tag ${target} changed after preflight (${existing}); expected ${expected}." >&2
    return 1
  fi
  echo "Immutable image tag ${target} already points to the expected digest."
}

previous_backend_latest="$(resolve_optional_digest "${backend}:latest")"
previous_frontend_latest="$(resolve_optional_digest "${frontend}:latest")"
backend_latest_started=0
frontend_latest_started=0
publish_latest=0
if [[ ${stable} -eq 1 ]]; then
  if [[ -n "${previous_backend_latest}" && -n "${previous_frontend_latest}" ]]; then
    publish_latest=1
  elif [[ -n "${previous_backend_latest}" || -n "${previous_frontend_latest}" ]]; then
    echo "Refusing latest promotion because only one image has a restorable previous latest digest." >&2
    exit 1
  else
    echo "Skipping latest promotion because no restorable previous latest pair exists. Version tags remain available."
  fi
fi

rollback() {
  local failures=0
  if [[ ${backend_latest_started} -eq 1 && -n "${previous_backend_latest}" ]]; then
    if ! promote "${backend}:latest" "${backend}" "${previous_backend_latest}"; then
      failures=$((failures + 1))
    fi
  fi
  if [[ ${frontend_latest_started} -eq 1 && -n "${previous_frontend_latest}" ]]; then
    if ! promote "${frontend}:latest" "${frontend}" "${previous_frontend_latest}"; then
      failures=$((failures + 1))
    fi
  fi
  if [[ ${failures} -ne 0 ]]; then
    echo "Container release rollback had ${failures} failure(s); inspect GHCR before retrying." >&2
  fi
  echo "Immutable release tags were preserved for a resumable workflow retry."
}

interrupt_with_rollback() {
  local status="$1"
  trap - ERR INT TERM
  rollback
  exit "${status}"
}

trap 'interrupt_with_rollback 130' INT
trap 'interrupt_with_rollback 143' TERM

run_promotion() {
  assert_immutable_compatible "${backend}:${GITHUB_REF_NAME}" "${BACKEND_DIGEST}" || return
  assert_immutable_compatible "${backend}:${version}" "${BACKEND_DIGEST}" || return
  assert_immutable_compatible "${frontend}:${GITHUB_REF_NAME}" "${FRONTEND_DIGEST}" || return
  assert_immutable_compatible "${frontend}:${version}" "${FRONTEND_DIGEST}" || return
  ensure_immutable_tag "${backend}:${GITHUB_REF_NAME}" "${backend}" "${BACKEND_DIGEST}" || return
  ensure_immutable_tag "${backend}:${version}" "${backend}" "${BACKEND_DIGEST}" || return
  ensure_immutable_tag "${frontend}:${GITHUB_REF_NAME}" "${frontend}" "${FRONTEND_DIGEST}" || return
  ensure_immutable_tag "${frontend}:${version}" "${frontend}" "${FRONTEND_DIGEST}" || return
  test "$(inspect_digest "${backend}:${GITHUB_REF_NAME}")" = "${BACKEND_DIGEST}" || return
  test "$(inspect_digest "${backend}:${version}")" = "${BACKEND_DIGEST}" || return
  test "$(inspect_digest "${frontend}:${GITHUB_REF_NAME}")" = "${FRONTEND_DIGEST}" || return
  test "$(inspect_digest "${frontend}:${version}")" = "${FRONTEND_DIGEST}" || return
  if [[ ${publish_latest} -ne 1 ]]; then
    return
  fi
  backend_latest_started=1
  promote "${backend}:latest" "${backend}" "${BACKEND_DIGEST}" || return
  frontend_latest_started=1
  promote "${frontend}:latest" "${frontend}" "${FRONTEND_DIGEST}" || return
}

set +e
run_promotion
status=$?
set -e
if [[ ${status} -ne 0 ]]; then
  trap - INT TERM
  rollback
  exit "${status}"
fi
trap - INT TERM
