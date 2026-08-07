#!/usr/bin/env bash
# Load the sampled dataset into all five graph platforms.
#
# Order is alphabetical (arangodb, cognodb, falkordb, memgraph, neo4j) - an
# arbitrary neutral sequence, NOT based on any performance expectation.
#
# Fairness rule: if any platform's load fails outright, reports nonzero
# failed_batches, or errors due to resource/disk/memory limits, STOP immediately.
# The dataset must then be shrunk and reloaded on ALL FIVE platforms (not just
# the one that failed) before benchmarking can proceed. This script does not
# auto-resize - it stops and reports so you can choose a new -max-edges.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

NODES="${NODES:-datasets/nodes.csv}"
RELS="${RELS:-datasets/relationships.csv}"
LOG_DIR="${ROOT}/results/load_logs"
mkdir -p "${LOG_DIR}"

# Alphabetical - arbitrary/neutral, not performance-based.
PLATFORMS=(arangodb cognodb falkordb memgraph neo4j)

json_field() {
  local file="$1" field="$2"
  if command -v python >/dev/null 2>&1; then
    python -c "import json; print(json.load(open(r'''${file}'''))['${field}'])"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "import json; print(json.load(open(r'''${file}'''))['${field}'])"
  else
    grep -o "\"${field}\"[[:space:]]*:[[:space:]]*[^,]*" "${file}" | head -n1 | sed 's/.*:[[:space:]]*//' | tr -d ' ",'
  fi
}

is_resource_error() {
  local msg
  msg="$(echo "$1" | tr '[:upper:]' '[:lower:]')"
  echo "${msg}" | grep -Eqi 'out of memory|oom|memory limit|disk (full|quota|space)|no space|resource|quota exceeded|storage.*limit|cannot allocate|heap|capacity'
}

fairness_stop() {
  local db="$1"
  local detail="$2"
  echo
  echo "==========================================================="
  echo "FAIRNESS STOP: platform '${db}' failed."
  echo "${detail}"
  echo
  echo "Per the fairness rule, the dataset must now be shrunk and"
  echo "reloaded on ALL FIVE platforms (not just '${db}') before"
  echo "benchmarking can proceed."
  echo "This script will NOT auto-resize. Choose a new -max-edges"
  echo "yourself, re-run sampling, then re-run scripts/load_all.sh."
  echo "==========================================================="
  exit 1
}

if [[ ! -f "${NODES}" || ! -f "${RELS}" ]]; then
  echo "ERROR: missing ${NODES} and/or ${RELS}. Run sampling first." >&2
  exit 1
fi

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

declare -a RESULT_FILES=()

echo "=== load_all: platforms in alphabetical/neutral order ==="
echo "nodes=${NODES}  rels=${RELS}"
echo

for db in "${PLATFORMS[@]}"; do
  echo "-----------------------------------------------------------"
  echo "Loading into: ${db}"
  echo "-----------------------------------------------------------"

  LOG="${LOG_DIR}/load_${db}_$(date -u +%Y%m%dT%H%M%SZ).log"

  # Stream progress live (do not buffer entire run into a variable).
  set +e
  go run ./cmd/load -db "${db}" -nodes "${NODES}" -rels "${RELS}" 2>&1 | tee "${LOG}"
  # tee preserves go's exit code poorly; use PIPESTATUS
  RC=${PIPESTATUS[0]}
  set -e

  if [[ ${RC} -ne 0 ]]; then
    DETAIL="Raw error / output (also in ${LOG}):"$'\n'"$(tail -n 80 "${LOG}")"
    if is_resource_error "$(cat "${LOG}")"; then
      DETAIL+=$'\n\n(Looks like a resource/disk/memory limit.)'
    fi
    fairness_stop "${db}" "${DETAIL}"
  fi

  LATEST="$(ls -1t results/load_${db}_*.json 2>/dev/null | head -n 1 || true)"
  if [[ -z "${LATEST}" ]]; then
    fairness_stop "${db}" "No result JSON was written under results/load_${db}_*.json"
  fi

  FAILED="$(json_field "${LATEST}" failed_batches)"
  if [[ "${FAILED}" != "0" ]]; then
    fairness_stop "${db}" "failed_batches=${FAILED} in ${LATEST}"$'\n'"Last log lines:"$'\n'"$(tail -n 40 "${LOG}")"
  fi

  RESULT_FILES+=("${LATEST}")
  echo "OK: ${db} -> ${LATEST}"
  echo
done

echo "=== ALL FIVE LOADS SUCCEEDED ==="
echo
printf "%-12s %10s %12s %12s %12s %12s %8s\n" \
  "platform" "nodes" "rels" "seconds" "nodes/s" "rels/s" "failed"
printf "%-12s %10s %12s %12s %12s %12s %8s\n" \
  "------------" "----------" "------------" "------------" "------------" "------------" "--------"

for f in "${RESULT_FILES[@]}"; do
  db="$(json_field "${f}" database)"
  nodes="$(json_field "${f}" node_count)"
  rels="$(json_field "${f}" rel_count)"
  secs="$(json_field "${f}" total_seconds)"
  nps="$(json_field "${f}" nodes_per_sec)"
  rps="$(json_field "${f}" rels_per_sec)"
  fail="$(json_field "${f}" failed_batches)"
  printf "%-12s %10s %12s %12s %12s %12s %8s\n" \
    "${db}" "${nodes}" "${rels}" "${secs}" "${nps}" "${rps}" "${fail}"
done
