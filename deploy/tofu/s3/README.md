# deploy/tofu/s3

OpenTofu config for lurker's media bucket: a private S3 bucket, a scoped IAM user for lurker, an ACM
certificate, a CloudFront distribution reading the bucket through OAC, and Route 53 alias records for
the CDN hostname.

Full walkthrough, including the manual alternative and the verification steps: [`S3_SETUP.md`](../../../S3_SETUP.md).

```sh
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars
tofu init
tofu plan
tofu apply          # 15–25 min, mostly the CloudFront deployment
tofu output
tofu output -raw lurker_secret_access_key
```

Notes:

- The Route 53 hosted zone for `route53_zone_name` must already exist; it is looked up, not created.
- `aws_iam_access_key` puts the secret access key in `terraform.tfstate` **in plaintext**. State and
  `*.tfvars` are gitignored (`deploy/tofu/.gitignore`) — keep the state file private or move it to an
  encrypted backend.
- Destroying this removes the bucket. `tofu destroy` will fail while objects remain (no
  `force_destroy`), which is deliberate.
