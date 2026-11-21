# Terraform module — Dockyard on AWS + Kubernetes

Provisions an S3 bucket + a scoped IAM user for Dockyard's storage backend,
then deploys the [Helm chart](../helm/dockyard) (from
`oci://ghcr.io/kgma74/charts/dockyard`) into an existing Kubernetes cluster,
wired to that bucket.

This module does **not** create the Kubernetes cluster itself — point it at
one you already have via `kube_config_path`/`kube_context` (defaults to your
current `kubectl` context).

## Usage

```bash
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars: bucket_name (must be globally unique) and auth_password at minimum

terraform init
terraform plan
terraform apply
```

## What it creates

- `aws_s3_bucket` + public-access block + SSE-S3 encryption + a lifecycle
  rule aborting stale multipart uploads after 7 days.
- A dedicated `aws_iam_user` with a policy scoped to exactly this bucket
  (`ListBucket`, `GetObject`, `PutObject`, `DeleteObject` — nothing else,
  no access to any other bucket or AWS service).
- A `kubernetes_namespace` (if it doesn't already exist).
- A `helm_release` deploying Dockyard with `registry.storage.backend: s3`
  pointed at the bucket above.

## Notes

- `auth_password` has no default on purpose — Terraform will prompt for it
  (or read it from `terraform.tfvars`/`TF_VAR_auth_password`) rather than
  ship a guessable one. Change it after first login regardless; this value
  only matters for the very first startup.
- Dockyard runs as **a single replica**, S3 or not: users, sessions,
  policies and the audit log live in SQLite on the pod's volume, and a second
  replica would serve a divergent copy. The chart refuses `replicaCount > 1`
  and `autoscaling.enabled` (since chart 0.6.0); `replica_count` and
  `autoscaling_enabled` are kept for compatibility and validated to `1` and
  `false`.
- The chart mounts a persistent volume (`persistence.*`, 10Gi by default)
  even with the S3 backend: the images go to the bucket, Dockyard's state and
  the Trivy cache stay on the volume. With scanning on, keep a few Gi for the
  Trivy databases (the Java DB alone is ~1 GB to download, larger unpacked).
- The IAM user's access key is passed to the chart as a Helm value: it ends
  up in the Terraform state and in the Helm release secret. Keep the state
  encrypted and access-controlled (see the remote state note below).
- `extra_helm_values` deep-merges over everything this module sets, so you
  can reach any value in `helm/dockyard/values.yaml` without this module
  needing a dedicated Terraform variable for it — e.g. TLS, cosign signing,
  Trivy scanning, single sign-on, resource limits:

  ```hcl
  extra_helm_values = {
    scan = { enabled = true }
    resources = {
      requests = { cpu = "250m", memory = "256Mi" }
      limits   = { memory = "1Gi" } # scans run trivy inside the pod
    }
  }
  ```
- State is left unconfigured (local backend) — add a `backend` block in
  `versions.tf` for remote state before using this against real
  infrastructure with more than one operator.
