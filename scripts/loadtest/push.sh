#!/usr/bin/env bash
# Pushes N repos, each with a manifest referencing a single blob of the
# given size, straight against the V2 API (no docker daemon needed) — used
# to fill a registry with a realistic number of repos/blobs before running
# pull-load.sh, and to measure raw push throughput.
#
# Usage: push.sh <base-url> <username> <password> <repo-count> <blob-size-bytes>
# Example: push.sh http://localhost:8080 admin changeme123 200 5242880   # 200 repos, 5 MiB blobs

set -euo pipefail

BASE="${1:?base url required, e.g. http://localhost:8080}"
USER="${2:?username required}"
PASS="${3:?password required}"
COUNT="${4:-100}"
SIZE="${5:-1048576}" # 1 MiB default

TOKEN=$(curl -sf -X POST "$BASE/api/admin/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}" | \
  node -e "process.stdin.on('data',d=>process.stdout.write(JSON.parse(d).token))")

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "Pushing $COUNT repos of ${SIZE} bytes each to $BASE ..."
START=$(date +%s)

for i in $(seq 1 "$COUNT"); do
  NAME="loadtest/repo-$i"
  BLOB="$TMP/blob"
  head -c "$SIZE" /dev/urandom > "$BLOB"
  DIGEST="sha256:$(sha256sum "$BLOB" | cut -d' ' -f1)"

  curl -sf -X POST "$BASE/v2/$NAME/blobs/uploads/?digest=$DIGEST" \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary "@$BLOB" -o /dev/null

  CONFIG_DIGEST="sha256:$(printf '{}' | sha256sum | cut -d' ' -f1)"
  curl -sf -X POST "$BASE/v2/$NAME/blobs/uploads/?digest=$CONFIG_DIGEST" \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary '{}' -o /dev/null

  MANIFEST=$(printf '{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"mediaType":"application/vnd.docker.container.image.v1+json","size":2,"digest":"%s"},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":%d,"digest":"%s"}]}' \
    "$CONFIG_DIGEST" "$SIZE" "$DIGEST")

  curl -sf -X PUT "$BASE/v2/$NAME/manifests/latest" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/vnd.docker.distribution.manifest.v2+json" \
    --data-binary "$MANIFEST" -o /dev/null

  if (( i % 20 == 0 )); then echo "  ...$i/$COUNT"; fi
done

END=$(date +%s)
ELAPSED=$((END - START))
echo "Pushed $COUNT repos in ${ELAPSED}s ($(awk "BEGIN{printf \"%.2f\", $COUNT/$ELAPSED}") repos/sec)"
