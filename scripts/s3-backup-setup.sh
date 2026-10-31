#!/usr/bin/env bash
# One-time Amazon S3 setup for the daily database backup. Run it from your
# machine with an admin profile, not on the server.
#
# What it does (safe to run again):
#   1. Creates the private bucket `thewealthlab-backups` in ap-southeast-2:
#      public access blocked, SSE-S3 encryption, versioning, and a lifecycle
#      rule that deletes backups after RETENTION_DAYS (default 90).
#      Versioning matters because the backup key can put objects: a leaked
#      key could overwrite a backup, but the old version stays for 30 days.
#   2. Creates IAM user `game-backup` that can only s3:PutObject into that
#      bucket (no list, get or delete), and an access key for it.
#   3. Prints the lines for /var/www/game/.env on the server.
#
# Needs the AWS CLI v2.
# Usage (Git Bash, in C:\Projects\go\game):
#   AWS_PROFILE=aws-admin scripts/s3-backup-setup.sh
set -euo pipefail

REGION=${AWS_REGION:-ap-southeast-2}
BUCKET=${BACKUP_BUCKET:-thewealthlab-backups}
RETENTION_DAYS=${RETENTION_DAYS:-90}
USER_NAME=game-backup
POLICY_NAME=s3-backup-game
APP_TAG=thewealthlab.au

ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
echo "AWS account $ACCOUNT, region $REGION"

# ── 1. Bucket ───────────────────────────────────────────────────────────────
if ! aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null; then
  aws s3api create-bucket --bucket "$BUCKET" --region "$REGION" \
    --create-bucket-configuration LocationConstraint="$REGION" >/dev/null
  echo "created bucket $BUCKET"
else
  echo "bucket $BUCKET exists"
fi
aws s3api put-public-access-block --bucket "$BUCKET" --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-encryption --bucket "$BUCKET" --server-side-encryption-configuration \
  '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
aws s3api put-bucket-versioning --bucket "$BUCKET" --versioning-configuration Status=Enabled
aws s3api put-bucket-lifecycle-configuration --bucket "$BUCKET" --lifecycle-configuration "$(cat <<JSON
{"Rules":[{"ID":"expire-backups","Status":"Enabled","Filter":{"Prefix":""},
  "Expiration":{"Days":$RETENTION_DAYS},
  "NoncurrentVersionExpiration":{"NoncurrentDays":30},
  "AbortIncompleteMultipartUpload":{"DaysAfterInitiation":2}}]}
JSON
)"
aws s3api put-bucket-tagging --bucket "$BUCKET" --tagging "TagSet=[{Key=app,Value=$APP_TAG}]"
echo "bucket $BUCKET: private, SSE-S3, versioned, backups deleted after $RETENTION_DAYS days"

# ── 2. IAM user + put-only policy ───────────────────────────────────────────
if ! aws iam get-user --user-name "$USER_NAME" >/dev/null 2>&1; then
  aws iam create-user --user-name "$USER_NAME" --tags Key=app,Value=$APP_TAG >/dev/null
  echo "created IAM user $USER_NAME"
else
  echo "IAM user $USER_NAME exists"
fi

POLICY_DOC=$(cat <<JSON
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["s3:PutObject"],
    "Resource": "arn:aws:s3:::${BUCKET}/*"
  }]
}
JSON
)
aws iam put-user-policy --user-name "$USER_NAME" --policy-name "$POLICY_NAME" --policy-document "$POLICY_DOC"
echo "policy $POLICY_NAME attached"

# Access key: only make one if the user has none (a secret can't be read again).
NKEYS=$(aws iam list-access-keys --user-name "$USER_NAME" --query 'length(AccessKeyMetadata)' --output text)
if [ "$NKEYS" = "0" ]; then
  read -r AKID SECRET < <(aws iam create-access-key --user-name "$USER_NAME" \
      --query 'AccessKey.[AccessKeyId,SecretAccessKey]' --output text)
  echo "created access key $AKID"
else
  AKID="(existing; make a new key in the IAM console if you lost the secret)"
  SECRET="(existing)"
  echo "user already has $NKEYS access key(s); not making another"
fi

# ── 3. Server .env lines ────────────────────────────────────────────────────
cat <<ENV

Add these to /var/www/game/.env on the server, then: sudo systemctl restart game

# Daily database backup to S3 (put-only IAM user)
BACKUP_S3_BUCKET=$BUCKET
BACKUP_AWS_ACCESS_KEY_ID=$AKID
BACKUP_AWS_SECRET_ACCESS_KEY=$SECRET
ENV
