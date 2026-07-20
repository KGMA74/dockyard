output "bucket_name" {
  description = "S3 bucket backing Dockyard's storage."
  value       = aws_s3_bucket.registry.id
}

output "bucket_arn" {
  value = aws_s3_bucket.registry.arn
}

output "iam_access_key_id" {
  description = "Access key ID for the dedicated Dockyard IAM user. The secret is not exposed as an output — it's wired directly into the Helm release."
  value       = aws_iam_access_key.dockyard.id
}

output "namespace" {
  value = kubernetes_namespace.dockyard.metadata[0].name
}

output "release_name" {
  value = helm_release.dockyard.name
}

output "release_status" {
  value = helm_release.dockyard.status
}
