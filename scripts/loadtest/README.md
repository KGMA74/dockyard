# Load testing

Scripts to fill a Dockyard instance with a realistic number of repos/blobs
and then hammer it with pulls, using [vegeta](https://github.com/tsenart/vegeta)
for the attack driver. No docker daemon required for the push side — it
talks to the V2 API directly with curl, so it works the same way against
`embedded`, `mirror`, or a container-less CI runner.

## Prerequisites

- `vegeta` on PATH (`go install github.com/tsenart/vegeta@latest`) — only
  needed for `pull-load.sh`.
- `curl`, `sha256sum`, `node` (used only to pull `.token` out of the login
  response JSON without adding a `jq` dependency).
- A running Dockyard instance, reachable and already migrated (first run
  creates the admin user from `AUTH_USERNAME`/`AUTH_PASSWORD`).

## Usage

```bash
# Fill the registry: 200 repos, 5 MiB blob each.
./scripts/loadtest/push.sh http://localhost:8080 admin changeme123 200 5242880

# Sustained pull load: 50 req/s for 30s against those same 200 repos.
./scripts/loadtest/pull-load.sh http://localhost:8080 admin changeme123 200 50 30s
```

Run the same pair of commands against a `REGISTRY_STORAGE_BACKEND=local`
instance and an S3-backed one (`REGISTRY_STORAGE_BACKEND=s3`, pointed at a
disposable bucket — **never** the bucket in your real `.env`) to compare.

## Baseline

No numbers are checked in here — throughput and latency depend entirely on
the machine, disk, and S3 provider/region you run against, so a committed
number would be misleading. Methodology instead:

1. Run `push.sh` once to seed the registry (not timed as part of the
   baseline — it's dominated by `/dev/urandom` and disk write cost, not the
   registry's read path).
2. Run `pull-load.sh` three times at increasing rate (e.g. 20, 50, 100
   req/s) for the same duration and repo count.
3. Record vegeta's `Success`, `p50`/`p95`/`p99` latency, and throughput from
   each report.
4. The number that matters is the rate at which `Success` drops below 100%
   or `p99` latency starts climbing — that's the instance's practical
   ceiling for that storage backend/hardware, not the raw numbers
   themselves (which won't reproduce on different hardware).

Re-run this after any change to the storage or manifest-resolution path
(e.g. `internal/storage`, `internal/admin/manifest.go`) that could plausibly
affect the hot read path, and compare against the previous run on the same
machine.
