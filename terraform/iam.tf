# A dedicated IAM user scoped to exactly this bucket — Dockyard's S3 backend
# needs GetObject/PutObject/DeleteObject/ListBucket and nothing else, so the
# policy denies everything outside those actions and this one bucket.
resource "aws_iam_user" "dockyard" {
  name = "${var.bucket_name}-dockyard"
  path = "/dockyard/"
}

data "aws_iam_policy_document" "dockyard" {
  statement {
    sid    = "ListBucket"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:GetBucketLocation",
    ]
    resources = [aws_s3_bucket.registry.arn]
  }

  statement {
    sid    = "ReadWriteObjects"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:AbortMultipartUpload",
      "s3:ListMultipartUploadParts",
    ]
    resources = ["${aws_s3_bucket.registry.arn}/*"]
  }
}

resource "aws_iam_user_policy" "dockyard" {
  name   = "dockyard-s3-access"
  user   = aws_iam_user.dockyard.name
  policy = data.aws_iam_policy_document.dockyard.json
}

resource "aws_iam_access_key" "dockyard" {
  user = aws_iam_user.dockyard.name
}
