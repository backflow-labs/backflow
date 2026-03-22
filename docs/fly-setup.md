# Fly.io Deployment Setup

## Prerequisites

- [flyctl](https://fly.io/docs/flyctl/install/) installed
- AWS infrastructure created (`make setup-aws`)
- GitHub repo with Actions enabled

## One-time setup

### 1. Create the Fly app

```bash
fly apps create backflow
```

### 2. Set secrets

Set all required env vars as Fly secrets. Pull values from your `.env`:

```bash
fly secrets set \
  BACKFLOW_DATABASE_URL="..." \
  ANTHROPIC_API_KEY="..." \
  OPENAI_API_KEY="..." \
  GITHUB_TOKEN="..." \
  AWS_REGION="us-east-1" \
  BACKFLOW_ECS_CLUSTER="..." \
  BACKFLOW_ECS_TASK_DEFINITION="..." \
  BACKFLOW_ECS_SUBNETS="..." \
  BACKFLOW_ECS_SECURITY_GROUPS="..." \
  BACKFLOW_CLOUDWATCH_LOG_GROUP="..." \
  BACKFLOW_S3_BUCKET="..."
```

Add integration secrets as needed (Discord, SMS/Twilio).

### 3. Set AWS credentials

The `backflow-fly` IAM user is created by `make setup-aws`. Generate access keys and set them as Fly secrets:

```bash
aws iam create-access-key --user-name backflow-fly
fly secrets set AWS_ACCESS_KEY_ID="..." AWS_SECRET_ACCESS_KEY="..."
```

### 4. Add FLY_API_TOKEN to GitHub Actions

Generate a Fly deploy token and add it as a GitHub Actions repository secret named `FLY_API_TOKEN`.

```bash
fly tokens create deploy -x 999999h
```

Add the token at: `Settings → Secrets and variables → Actions → New repository secret`

### 5. Deploy

Merge to main — CI runs tests then deploys automatically. Or deploy manually:

```bash
fly deploy --remote-only
```

## Verify

```bash
fly status                                          # machine running
fly logs                                            # no startup errors
curl https://backflow.fly.dev/health                # 200 (root health, always open)
curl https://backflow.fly.dev/api/v1/health         # 403 (API restricted)
curl https://backflow.fly.dev/api/v1/tasks          # 403 (API restricted)
```

## API access restriction

`BACKFLOW_RESTRICT_API=true` is set in `fly.toml`'s `[env]`. This blocks all `/api/v1/*` endpoints with a 403. Webhook paths (`/webhooks/discord`, `/webhooks/sms/inbound`) and the root `/health` are unaffected.

## Configuration

App configuration lives in `fly.toml`. Secrets are managed via `fly secrets`. See `internal/config/config.go` for all supported env vars and their defaults.

## Useful commands

```bash
fly status              # App and machine status
fly logs                # Stream logs
fly ssh console         # SSH into the machine
fly secrets list        # List configured secrets
fly scale memory 512    # Resize memory (if 256MB is insufficient)
```
