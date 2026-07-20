resource "kubernetes_namespace" "dockyard" {
  metadata {
    name = var.namespace
  }
}

locals {
  base_values = {
    replicaCount = var.replica_count

    registry = {
      mode = "embedded"
      storage = {
        backend = "s3"
      }
      s3 = {
        endpoint  = "s3.${var.aws_region}.amazonaws.com"
        bucket    = aws_s3_bucket.registry.id
        region    = var.aws_region
        secure    = "true"
        accessKey = aws_iam_access_key.dockyard.id
        secretKey = aws_iam_access_key.dockyard.secret
      }
    }

    auth = {
      username = var.auth_username
      password = var.auth_password
    }

    autoscaling = {
      enabled = var.autoscaling_enabled
    }

    ingress = {
      enabled     = var.ingress_enabled
      className   = var.ingress_class_name
      hosts = var.ingress_enabled ? [
        {
          host = var.ingress_host
          paths = [
            { path = "/", pathType = "Prefix" },
          ]
        },
      ] : []
    }
  }
}

resource "helm_release" "dockyard" {
  name       = var.release_name
  namespace  = kubernetes_namespace.dockyard.metadata[0].name
  repository = "oci://ghcr.io/kgma74/charts"
  chart      = "dockyard"
  version    = var.chart_version != "" ? var.chart_version : null

  # Helm deep-merges multiple value files in order, so extra_helm_values can
  # override a single nested key — e.g. {registry = {s3 = {secure = ...}}} —
  # without having to restate every other key base_values already sets.
  values = [
    yamlencode(local.base_values),
    yamlencode(var.extra_helm_values),
  ]
}
