# S3 media setup

Lurker optimizes uploaded images locally and pushes the finished bytes to an S3 bucket. Other IRC
members fetch them over a CloudFront custom domain (`cdn.example.com`), so lurker never has to open a
port to the public internet.

Topology:

```
web/apple client ──POST /api/upload──▶ lurker (tailnet only)
                                         │ optimize + SHA-256 dedup
                                         ▼
                                    S3 bucket (fully private, no public access)
                                         ▲ OAC (signed reads)
                                         │
IRC member's browser ──GET──▶ CloudFront (cdn.example.com, ACM cert) ─┘
```

Two ways to get there: [OpenTofu](#option-a-opentofu-recommended) (everything except the AWS
credentials for tofu itself), or [by hand](#option-b-by-hand). Either way, finish with
[Configure lurker](#4-configure-lurker) and [Verify](#5-verify-before-lurker-supports-it).

Replace `lurker-media`, `eu-north-1`, `cdn.example.com`, and `example.com` with your own values
throughout.

---

## Option A: OpenTofu (recommended)

The config lives in `deploy/tofu/s3/`. It creates the bucket, the private-access settings, the IAM user
+ access key for lurker, the ACM certificate, the CloudFront distribution with OAC, and the Route 53
records.

Prerequisites:

- `tofu` installed (`brew install opentofu`)
- AWS credentials with admin-ish rights in your shell (`AWS_PROFILE=...`, or `aws sso login`)
- The domain's hosted zone already in Route 53 (this config looks the zone up by name; it does not
  create it)

```sh
cd deploy/tofu/s3
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars     # bucket_name, aws_region, cdn_hostname, route53_zone_name
tofu init
tofu plan
tofu apply
```

The `apply` waits on ACM DNS validation and the CloudFront deployment — **budget 15–25 minutes**, most
of it CloudFront. When it finishes:

```sh
tofu output                                    # endpoint, bucket, region, public_base_url
tofu output -raw lurker_access_key_id
tofu output -raw lurker_secret_access_key      # sensitive; goes in your env, not config.yaml
```

Note: the secret access key is stored in `terraform.tfstate` in plaintext. `deploy/tofu/.gitignore`
keeps state and `*.tfvars` out of git — keep that state file private, or move it to an encrypted
backend.

Skip to [Configure lurker](#4-configure-lurker).

---

## Option B: by hand

### 1. Create the bucket

Console: S3 → Create bucket → name `lurker-media`, region `eu-north-1`, and leave **Block all public
access ON**. Object Ownership: **ACLs disabled (bucket owner enforced)**.

```sh
aws s3api create-bucket \
  --bucket lurker-media \
  --region eu-north-1 \
  --create-bucket-configuration LocationConstraint=eu-north-1

aws s3api put-bucket-ownership-controls \
  --bucket lurker-media \
  --ownership-controls 'Rules=[{ObjectOwnership=BucketOwnerEnforced}]'

aws s3api put-public-access-block \
  --bucket lurker-media \
  --public-access-block-configuration \
    'BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true'
```

The bucket stays private for good: nothing reads it directly. Only the CloudFront distribution created
below gets `s3:GetObject`, via a bucket policy scoped to that one distribution.

`BlockPublicPolicy=true` still permits the CloudFront service-principal policy in step 3 — that policy
is not "public" as far as S3 is concerned, because it's scoped to a service principal and a specific
distribution ARN.

Optional but recommended — abort incomplete multipart uploads after 7 days:

```sh
aws s3api put-bucket-lifecycle-configuration --bucket lurker-media \
  --lifecycle-configuration '{"Rules":[{"ID":"abort-incomplete-multipart","Status":"Enabled","Filter":{},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":7}}]}'
```

**CORS is not needed.** Lurker PUTs from the server side and browsers only ever `GET` the finished
image as an `<img>` source. Don't add a CORS policy.

### 2. IAM user for lurker

Lurker needs exactly four actions. `s3:ListBucket` is what the boot-time health probe
(`BucketExists`) uses.

```sh
aws iam create-user --user-name lurker-media

cat > /tmp/lurker-media-policy.json <<'JSON'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "Objects",
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::lurker-media/*"
    },
    {
      "Sid": "BucketProbe",
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::lurker-media"
    }
  ]
}
JSON

aws iam put-user-policy --user-name lurker-media \
  --policy-name lurker-media-rw --policy-document file:///tmp/lurker-media-policy.json

aws iam create-access-key --user-name lurker-media   # save AccessKeyId + SecretAccessKey now
```

The secret is shown once. It becomes `AWS_SECRET_ACCESS_KEY_LURKER` in lurker's environment.

### 3. CloudFront + ACM + Route 53 for `cdn.example.com`

**ACM certificate — must be in `us-east-1`**, regardless of the bucket's region. CloudFront reads certs
only from there.

```sh
aws acm request-certificate --region us-east-1 \
  --domain-name cdn.example.com --validation-method DNS
# then, for the returned CertificateArn:
aws acm describe-certificate --region us-east-1 --certificate-arn <arn> \
  --query 'Certificate.DomainValidationOptions[0].ResourceRecord'
```

Add the returned `CNAME` name/value as a record in your Route 53 hosted zone (console: Route 53 →
hosted zone → Create record, or use the "Create records in Route 53" button on the ACM page, which is
the fastest path). Validation flips to `ISSUED` a few minutes later.

**Distribution.** Console: CloudFront → Create distribution.

- Origin: the `lurker-media` S3 bucket (pick the bucket, not a custom origin).
- Origin access: **Origin access control (OAC)** → create a new OAC, signing behavior "Sign requests".
  Legacy OAI is deprecated; don't use it.
- Viewer protocol policy: **Redirect HTTP to HTTPS**.
- Allowed methods: **GET, HEAD**.
- Cache policy: managed **CachingOptimized**. Our objects are immutable (random 10-char base62 ids,
  never rewritten) and already carry `Cache-Control: public, max-age=31536000, immutable`, so long edge
  caching is a pure win.
- Compress objects automatically: on.
- Alternate domain name (CNAME): `cdn.example.com`. Custom SSL certificate: the ACM cert from above.
- Security policy: **TLSv1.2_2021**. IPv6: on. Default root object: leave empty.

CloudFront then offers to copy the bucket policy for you — take it. It looks like this:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "AllowCloudFrontServicePrincipalRead",
    "Effect": "Allow",
    "Principal": { "Service": "cloudfront.amazonaws.com" },
    "Action": "s3:GetObject",
    "Resource": "arn:aws:s3:::lurker-media/*",
    "Condition": {
      "StringEquals": {
        "AWS:SourceArn": "arn:aws:cloudfront::<account-id>:distribution/<distribution-id>"
      }
    }
  }]
}
```

```sh
aws s3api put-bucket-policy --bucket lurker-media --policy file:///tmp/bucket-policy.json
```

**Route 53 alias.** In the hosted zone, create an **A** record and an **AAAA** record for
`cdn.example.com`, both as *Alias → Alias to CloudFront distribution* → your distribution. (Alias
records, not CNAMEs — they resolve at the apex-friendly Route 53 level and cost nothing.)

Distribution status goes `Deploying` → `Enabled` in roughly 15–20 minutes. Nothing works until it does.

---

## 4. Configure lurker

Lurker requires the storage backend to be named explicitly. There is no default and no fallback: if
the `media:` block is missing, uploads are disabled; if it's present but incomplete, the process
refuses to start.

Add to `config.yaml`:

```yaml
media:
  backend: s3              # "s3" | "disk"
  max_bytes: 20971520      # 20 MiB, optional
  s3:
    endpoint: s3.eu-north-1.amazonaws.com    # no scheme
    region: eu-north-1
    bucket: lurker-media
    access_key_id: ${AWS_ACCESS_KEY_ID_LURKER}
    secret_access_key: ${AWS_SECRET_ACCESS_KEY_LURKER}
    public_base_url: https://cdn.example.com  # CloudFront, NOT the S3 host
    prefix: ""             # optional key prefix, e.g. "media"
    path_style: false      # false for AWS S3
```

`${VAR}` is expanded from the environment, so no secret lands in the file. Export them where lurker
runs:

```sh
# local dev (task dev / task dev-web)
export AWS_ACCESS_KEY_ID_LURKER=AKIA...
export AWS_SECRET_ACCESS_KEY_LURKER=...
```

```yaml
# compose.yaml, for the deployed container
services:
  lurker:
    environment:
      AWS_ACCESS_KEY_ID_LURKER: ${AWS_ACCESS_KEY_ID_LURKER}
      AWS_SECRET_ACCESS_KEY_LURKER: ${AWS_SECRET_ACCESS_KEY_LURKER}
```

`public_base_url` must be the CloudFront hostname. It is what gets pasted into IRC, and with
`backend: s3` there is no local copy of the file to fall back on.

At boot lurker probes the bucket. On success:

```
INFO media storage backend=s3 bucket=lurker-media public_base_url=https://cdn.example.com
```

On failure it logs an ERROR and keeps running — IRC still connects, but every upload returns `502`
until the credentials or bucket are fixed. It never silently writes to local disk instead.

## 5. Verify (before lurker supports it)

Each step isolates one thing.

```sh
# a) IAM credentials + bucket write
AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID_LURKER \
AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY_LURKER \
aws s3 cp ./test.jpg s3://lurker-media/probe.jpg \
  --content-type image/jpeg \
  --cache-control 'public, max-age=31536000, immutable'

# b) ACM + OAC + DNS: expect 200, image/jpeg, the immutable cache-control
curl -I https://cdn.example.com/probe.jpg

# c) bucket really is private: expect 403
curl -I https://lurker-media.s3.eu-north-1.amazonaws.com/probe.jpg

# d) clean up
AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID_LURKER \
AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY_LURKER \
aws s3 rm s3://lurker-media/probe.jpg
```

If (b) returns 403 with an `X-Cache: Error from cloudfront` header, the bucket policy or OAC is wrong.
If it returns an SSL error, the ACM cert isn't attached or the distribution is still deploying.

## Tradeoffs worth knowing

- **Anything uploaded is publicly fetchable** by anyone with the URL. That's the point — other IRC
  members need to load it. Nothing private should be uploaded. The 10-char base62 ids
  (~8.4 × 10^17 keyspace) make URLs unguessable, but they are not a secret.
- **Deletes don't purge the edge.** `DELETE /api/media/{id}` removes the row and the S3 object, but a
  copy can stay cached in CloudFront until its TTL expires. There is no automatic invalidation; run
  one by hand if a delete needs to take effect immediately.
- **Orphan cleanup is manual.** A failed upload leaves no row and no object (lurker refuses to record a
  half-published upload), but nothing reconciles the bucket against `media.db` on a schedule.
- **Cost:** storage plus CloudFront egress. The immutable `Cache-Control` means repeat views mostly
  hit the edge rather than S3.
