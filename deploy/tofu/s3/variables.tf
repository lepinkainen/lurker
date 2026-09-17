variable "aws_region" {
  description = "Region for the media bucket. The ACM certificate is always created in us-east-1 regardless."
  type        = string
  default     = "eu-north-1"
}

variable "bucket_name" {
  description = "S3 bucket name for uploaded media. Globally unique."
  type        = string
}

variable "iam_user_name" {
  description = "IAM user lurker authenticates as when writing to the bucket."
  type        = string
  default     = "lurker-media"
}

variable "cdn_hostname" {
  description = "Public hostname served by CloudFront, e.g. cdn.example.com. Becomes media.s3.public_base_url."
  type        = string
}

variable "route53_zone_name" {
  description = "Route 53 hosted zone the cdn_hostname lives in, e.g. example.com. Must already exist."
  type        = string
}

variable "abort_incomplete_multipart_days" {
  description = "Days after which incomplete multipart uploads are aborted."
  type        = number
  default     = 7
}

variable "tags" {
  description = "Tags applied to every taggable resource."
  type        = map(string)
  default     = { project = "lurker" }
}
