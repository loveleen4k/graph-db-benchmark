#!/usr/bin/env bash
# Download the SNAP Pokec relationships graph into datasets/raw/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RAW_DIR="${ROOT}/datasets/raw"
URL="https://snap.stanford.edu/data/soc-pokec-relationships.txt.gz"
OUT="${RAW_DIR}/soc-pokec-relationships.txt.gz"

mkdir -p "${RAW_DIR}"

# Soft completeness check: remote Content-Length is currently ~126 MiB.
# Prefer gzip -t validation when the file already exists.
if [[ -f "${OUT}" ]]; then
  if gzip -t "${OUT}" 2>/dev/null; then
    echo "Already present, skipping download: ${OUT}"
    ls -lh "${OUT}"
    exit 0
  fi
  echo "Existing file failed gzip integrity check; re-downloading..."
  rm -f "${OUT}"
fi

echo "Downloading ${URL}"
echo "  -> ${OUT}"

ATTEMPTS=5
for i in $(seq 1 "${ATTEMPTS}"); do
  set +e
  curl -L --progress-bar -C - -o "${OUT}" "${URL}"
  RC=$?
  set -e
  if [[ ${RC} -eq 33 ]]; then
    echo "Server rejected resume; restarting full download..."
    rm -f "${OUT}"
    continue
  fi
  if [[ ${RC} -eq 0 ]]; then
    if gzip -t "${OUT}" 2>/dev/null; then
      ls -lh "${OUT}"
      echo "Download complete."
      exit 0
    fi
    echo "Download finished but gzip integrity failed; retrying (${i}/${ATTEMPTS})..."
    rm -f "${OUT}"
  else
    echo "curl exit ${RC}; retrying (${i}/${ATTEMPTS})..."
  fi
  sleep 2
done

echo "ERROR: failed to download a valid gzip after ${ATTEMPTS} attempts" >&2
exit 1
