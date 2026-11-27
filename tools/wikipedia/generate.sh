#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_ROOT}"

START_MARKER="$(mktemp)"
trap 'rm -f "${START_MARKER}"' EXIT

if [[ $# -gt 0 ]]; then
  bazel run //tools/wikipedia -- "$@"
else
  bazel run //tools/wikipedia
fi

BAZEL_BIN="$(bazel info bazel-bin)"
WIKI_OUT_DIR="${BAZEL_BIN}/tools/wikipedia"

if [[ ! -d "${WIKI_OUT_DIR}" ]]; then
  echo "Error: Bazel output directory not found: ${WIKI_OUT_DIR}" >&2
  exit 1
fi

GENERATED_TSD=""
while IFS= read -r -d '' candidate; do
  if [[ -z "${GENERATED_TSD}" || "${candidate}" -nt "${GENERATED_TSD}" ]]; then
    GENERATED_TSD="${candidate}"
  fi
done < <(find "${WIKI_OUT_DIR}" -type f -name '*.tsd' -newer "${START_MARKER}" -print0)

if [[ -z "${GENERATED_TSD}" ]]; then
  echo "Error: no newly generated .tsd dictionary found in ${WIKI_OUT_DIR}" >&2
  exit 1
fi

FILENAME="$(basename "${GENERATED_TSD}")"
if [[ "${FILENAME}" =~ (^|[^0-9])([0-9]{4})[0-9]{4}([^0-9]|$) ]]; then
  YEAR="${BASH_REMATCH[2]}"
else
  echo "Error: could not extract YYYYMMDD dump date from '${FILENAME}'" >&2
  exit 1
fi

RELEASE_DIR="${REPO_ROOT}/release"
mkdir -p "${RELEASE_DIR}"

DEST="${RELEASE_DIR}/English Wikipedia ${YEAR}.tsd"
mv "${GENERATED_TSD}" "${DEST}"

echo "Moved ${GENERATED_TSD} -> ${DEST}"
