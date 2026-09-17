# These map 1:1 onto the media.s3 keys in lurker's config.yaml.

output "bucket" {
  description = "media.s3.bucket"
  value       = aws_s3_bucket.media.id
}

output "region" {
  description = "media.s3.region"
  value       = var.aws_region
}

output "s3_endpoint" {
  description = "media.s3.endpoint (no scheme)"
  value       = "s3.${var.aws_region}.amazonaws.com"
}

output "public_base_url" {
  description = "media.s3.public_base_url — the CloudFront hostname, not the S3 host"
  value       = "https://${var.cdn_hostname}"
}

output "cloudfront_domain_name" {
  description = "Distribution hostname the cdn_hostname aliases to; useful for debugging DNS"
  value       = aws_cloudfront_distribution.media.domain_name
}

output "lurker_access_key_id" {
  description = "Export as AWS_ACCESS_KEY_ID_LURKER"
  value       = aws_iam_access_key.lurker.id
}

output "lurker_secret_access_key" {
  description = "Export as AWS_SECRET_ACCESS_KEY_LURKER; read with: tofu output -raw lurker_secret_access_key"
  value       = aws_iam_access_key.lurker.secret
  sensitive   = true
}
