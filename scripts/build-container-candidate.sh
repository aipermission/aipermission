#!/usr/bin/env bash
set -euo pipefail

name="$1"
context="$2"
shift 2
image="ghcr.io/aipermission/aipermission-${name}"
candidate="${image}:candidate-${GITHUB_SHA}"
release="${image}:${GITHUB_REF_NAME}"
metadata="${RUNNER_TEMP}/${name}-image-metadata.json"
attestation_type="https://aipermission.dev/attestations/release-source/v1"

resolve_optional_digest() {
  local reference="$1" output status digest
  set +e
  output="$(docker buildx imagetools inspect "${reference}" 2>&1)"
  status=$?
  set -e
  if [[ ${status} -eq 0 ]]; then
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

verify_release_source() {
  local reference="$1"
  local identity="https://github.com/${GITHUB_REPOSITORY}/.github/workflows/publish-images.yml@${GITHUB_REF}"
  local issuer="https://token.actions.githubusercontent.com"
  cosign verify \
    --certificate-identity "${identity}" \
    --certificate-oidc-issuer "${issuer}" \
    "${reference}" >/dev/null
  cosign verify-attestation \
    --type "${attestation_type}" \
    --certificate-identity "${identity}" \
    --certificate-oidc-issuer "${issuer}" \
    "${reference}" |
    jq -s -e \
      --arg repository "${GITHUB_REPOSITORY}" \
      --arg commit "${GITHUB_SHA}" \
      --arg workflow ".github/workflows/publish-images.yml" \
      --arg ref "${GITHUB_REF}" \
      'any(.[]; (.payload | @base64d | fromjson | .predicate) as $p |
        $p.repository == $repository and $p.commit == $commit and $p.workflow == $workflow and $p.ref == $ref)' >/dev/null
}

digest="$(resolve_optional_digest "${release}")"
if [[ -n "${digest}" ]]; then
  verify_release_source "${image}@${digest}"
  echo "Reusing verified immutable release image ${release}@${digest}."
else
  docker buildx build \
    --platform linux/amd64 \
    --provenance=mode=max \
    --sbom=true \
    --metadata-file "${metadata}" \
    --tag "${candidate}" \
    "$@" \
    --push \
    "${context}"
  digest="$(jq -r '."containerimage.digest"' "${metadata}")"
fi

test "${digest#sha256:}" != "${digest}"
echo "${name}_digest=${digest}" >> "${GITHUB_OUTPUT}"
echo "${name}_reference=${image}@${digest}" >> "${GITHUB_OUTPUT}"
