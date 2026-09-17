terraform {
  required_version = ">= 1.8"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# CloudFront reads its viewer certificate only from us-east-1, whatever region
# the bucket lives in.
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"
}

data "aws_route53_zone" "this" {
  name         = var.route53_zone_name
  private_zone = false
}

# ---------------------------------------------------------------------------
# Bucket — fully private. Only the CloudFront distribution below can read it.
# ---------------------------------------------------------------------------

resource "aws_s3_bucket" "media" {
  bucket = var.bucket_name
  tags   = var.tags
}

resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id

  rule {
    object_ownership = "BucketOwnerEnforced" # ACLs disabled entirely
  }
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket = aws_s3_bucket.media.id

  # All four on. The CloudFront service-principal policy below is not a
  # "public" policy as far as S3 is concerned, so BlockPublicPolicy does not
  # conflict with it.
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id

  rule {
    id     = "abort-incomplete-multipart"
    status = "Enabled"

    filter {}

    abort_incomplete_multipart_upload {
      days_after_initiation = var.abort_incomplete_multipart_days
    }
  }
}

data "aws_iam_policy_document" "bucket_cloudfront_read" {
  statement {
    sid     = "AllowCloudFrontServicePrincipalRead"
    effect  = "Allow"
    actions = ["s3:GetObject"]

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    resources = ["${aws_s3_bucket.media.arn}/*"]

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.media.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "media" {
  bucket = aws_s3_bucket.media.id
  policy = data.aws_iam_policy_document.bucket_cloudfront_read.json

  depends_on = [aws_s3_bucket_public_access_block.media]
}

# ---------------------------------------------------------------------------
# IAM user lurker writes as. Four actions, nothing more.
# ---------------------------------------------------------------------------

resource "aws_iam_user" "lurker" {
  name = var.iam_user_name
  tags = var.tags
}

data "aws_iam_policy_document" "lurker_rw" {
  statement {
    sid     = "Objects"
    effect  = "Allow"
    actions = ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"]

    resources = ["${aws_s3_bucket.media.arn}/*"]
  }

  statement {
    sid     = "BucketProbe" # ListBucket backs the boot-time BucketExists probe
    effect  = "Allow"
    actions = ["s3:ListBucket"]

    resources = [aws_s3_bucket.media.arn]
  }
}

resource "aws_iam_user_policy" "lurker_rw" {
  name   = "${var.iam_user_name}-rw"
  user   = aws_iam_user.lurker.name
  policy = data.aws_iam_policy_document.lurker_rw.json
}

# The secret lands in tofu state in plaintext. deploy/tofu/.gitignore keeps
# state out of git; keep it private or move to an encrypted backend.
resource "aws_iam_access_key" "lurker" {
  user = aws_iam_user.lurker.name
}

# ---------------------------------------------------------------------------
# ACM certificate for the CDN hostname (us-east-1, DNS-validated)
# ---------------------------------------------------------------------------

resource "aws_acm_certificate" "cdn" {
  provider = aws.us_east_1

  domain_name       = var.cdn_hostname
  validation_method = "DNS"
  tags              = var.tags

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cdn_cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.cdn.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }

  zone_id         = data.aws_route53_zone.this.zone_id
  name            = each.value.name
  type            = each.value.type
  records         = [each.value.record]
  ttl             = 60
  allow_overwrite = true
}

resource "aws_acm_certificate_validation" "cdn" {
  provider = aws.us_east_1

  certificate_arn         = aws_acm_certificate.cdn.arn
  validation_record_fqdns = [for r in aws_route53_record.cdn_cert_validation : r.fqdn]
}

# ---------------------------------------------------------------------------
# CloudFront distribution — the only public entry point
# ---------------------------------------------------------------------------

resource "aws_cloudfront_origin_access_control" "media" {
  name                              = "${var.bucket_name}-oac"
  description                       = "OAC for the lurker media bucket"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# Managed CachingOptimized: uploads are immutable (random base62 ids, never
# rewritten) and carry Cache-Control: public, max-age=31536000, immutable.
data "aws_cloudfront_cache_policy" "caching_optimized" {
  name = "Managed-CachingOptimized"
}

resource "aws_cloudfront_distribution" "media" {
  enabled         = true
  is_ipv6_enabled = true
  comment         = "lurker media (${var.bucket_name})"
  aliases         = [var.cdn_hostname]
  price_class     = "PriceClass_100"
  tags            = var.tags

  origin {
    origin_id                = "s3-${var.bucket_name}"
    domain_name              = aws_s3_bucket.media.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.media.id
  }

  default_cache_behavior {
    target_origin_id       = "s3-${var.bucket_name}"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true
    cache_policy_id        = data.aws_cloudfront_cache_policy.caching_optimized.id
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.cdn.certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }
}

resource "aws_route53_record" "cdn_a" {
  zone_id = data.aws_route53_zone.this.zone_id
  name    = var.cdn_hostname
  type    = "A"

  alias {
    name                   = aws_cloudfront_distribution.media.domain_name
    zone_id                = aws_cloudfront_distribution.media.hosted_zone_id
    evaluate_target_health = false
  }
}

resource "aws_route53_record" "cdn_aaaa" {
  zone_id = data.aws_route53_zone.this.zone_id
  name    = var.cdn_hostname
  type    = "AAAA"

  alias {
    name                   = aws_cloudfront_distribution.media.domain_name
    zone_id                = aws_cloudfront_distribution.media.hosted_zone_id
    evaluate_target_health = false
  }
}
