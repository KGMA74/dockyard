variable "aws_region" {
  description = "AWS region for the S3 bucket and IAM resources."
  type        = string
  default     = "us-east-1"
}

variable "bucket_name" {
  description = "Name of the S3 bucket backing Dockyard's storage (must be globally unique)."
  type        = string
}

variable "bucket_force_destroy" {
  description = "Allow `terraform destroy` to delete the bucket even if it still has objects. Leave false in production."
  type        = bool
  default     = false
}

variable "kube_config_path" {
  description = "Path to a kubeconfig file. Leave default to use the current kubectl context."
  type        = string
  default     = "~/.kube/config"
}

variable "kube_context" {
  description = "kubeconfig context to deploy into. Empty string = kubeconfig's current-context."
  type        = string
  default     = ""
}

variable "namespace" {
  description = "Kubernetes namespace Dockyard is deployed into (created if it doesn't exist)."
  type        = string
  default     = "registry"
}

variable "release_name" {
  description = "Helm release name."
  type        = string
  default     = "dockyard"
}

variable "chart_version" {
  description = "Dockyard Helm chart version to deploy, from oci://ghcr.io/kgma74/charts/dockyard. Empty = latest."
  type        = string
  default     = ""
}

variable "auth_username" {
  description = "Initial admin username."
  type        = string
  default     = "admin"
}

variable "auth_password" {
  description = "Initial admin password (first startup only — change it after logging in). Required, no default on purpose."
  type        = string
  sensitive   = true
}

variable "replica_count" {
  description = "Number of Dockyard replicas. Keep at 1 unless autoscaling_enabled is also true (S3 backend required for more than 1)."
  type        = number
  default     = 1
}

variable "autoscaling_enabled" {
  description = "Enable the Horizontal Pod Autoscaler. Requires an S3 storage backend, which this module always configures, so it's safe to turn on."
  type        = bool
  default     = false
}

variable "ingress_enabled" {
  description = "Expose Dockyard through an Ingress."
  type        = bool
  default     = false
}

variable "ingress_host" {
  description = "Hostname for the Ingress (only used when ingress_enabled is true)."
  type        = string
  default     = ""
}

variable "ingress_class_name" {
  description = "IngressClass name (e.g. \"nginx\", \"traefik\")."
  type        = string
  default     = ""
}

variable "extra_helm_values" {
  description = "Additional Helm values merged on top of this module's defaults (e.g. resources, tls, signing). Deep-merged last, so it can override anything set here."
  type        = any
  default     = {}
}
