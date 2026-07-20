#!/usr/bin/env bash
# Runs a sustained pull load against repos created by push.sh, using vegeta
# (https://github.com/tsenart/vegeta). Reports latency percentiles and
# throughput — compare local-filesystem vs S3-backed runs by pointing the
# same script at two differently configured instances.
#
# Usage: pull-load.sh <base-url> <username> <password> <repo-count> <rate-per-sec> <duration>
# Example: pull-load.sh http://localhost:8080 admin changeme123 200 50 30s

set -euo pipefail

BASE="${1:?base url required}"
USER="${2:?username required}"
PASS="${3:?password required}"
COUNT="${4:-100}"
RATE="${5:-50}"
DURATION="${6:-30s}"

if ! command -v vegeta >/dev/null 2>&1; then
  echo "vegeta is not installed — see https://github.com/tsenart/vegeta#install" >&2
  exit 1
fi

TOKEN=$(curl -sf -X POST "$BASE/api/admin/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}" | \
  node -e "process.stdin.on('data',d=>process.stdout.write(JSON.parse(d).token))")

TARGETS=$(mktemp)
trap 'rm -f "$TARGETS"' EXIT

for i in $(seq 1 "$COUNT"); do
  {
    echo "GET $BASE/v2/loadtest/repo-$i/manifests/latest"
    echo "Authorization: Bearer $TOKEN"
    echo "Accept: application/vnd.docker.distribution.manifest.v2+json"
    echo
  } >> "$TARGETS"
done

echo "Pulling $COUNT manifests at ${RATE}/sec for $DURATION ..."
vegeta attack -targets="$TARGETS" -rate="$RATE" -duration="$DURATION" | \
  vegeta report -type=text
