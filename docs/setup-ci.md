# CI Setup (GitHub Actions)

The `docker-deploy.yml` workflow builds a multi-arch agent image and pushes it to ECR whenever files under `docker/` change on `main`.

## Prerequisites

Run `make setup-aws` with `BACKFLOW_GITHUB_REPO` set to create the required AWS resources automatically:

```bash
BACKFLOW_GITHUB_REPO=your-org/backflow make setup-aws
```

This creates:

- **GitHub Actions OIDC provider** — allows GHA to authenticate to AWS without long-lived credentials
- **`backflow-ci-deploy` IAM role** — assumable only from the `main` branch of your repo, scoped to ECR push
- **`backflow-ci-ecr-push` IAM policy** — `ecr:GetAuthorizationToken` + image push permissions for the `backflow-agent` repository

If you've already run `make setup-aws` before, re-running with `BACKFLOW_GITHUB_REPO` set will add the CI resources without recreating existing infrastructure.

## GitHub Repository Configuration

### Secrets

Add one secret in **Settings > Secrets and variables > Actions > Repository secrets**:

| Secret | Value | Description |
|---|---|---|
| `AWS_ROLE_ARN` | `arn:aws:iam::<ACCOUNT_ID>:role/backflow-ci-deploy` | The role ARN printed by `make setup-aws` |

### Environment variables (already set in workflow)

These are hardcoded in `docker-deploy.yml` and don't need to be configured:

| Variable | Value | Description |
|---|---|---|
| `AWS_REGION` | `us-east-1` | Must match the region used in `make setup-aws` |
| `ECR_REPOSITORY` | `backflow-agent` | Must match the ECR repo name |

If you use a different region or repo name, update the `env:` block in `.github/workflows/docker-deploy.yml`.

## How It Works

1. Push to `main` touches a file under `docker/` — workflow triggers
2. GHA requests an OIDC token and assumes `backflow-ci-deploy` via `aws-actions/configure-aws-credentials`
3. Logs in to ECR, builds a `linux/amd64` + `linux/arm64` image with Buildx
4. Pushes the image tagged `latest` to ECR
5. Build layers are cached via GitHub Actions cache (`type=gha`)

## Verifying

Push a change to `docker/` on `main` and check the **Actions** tab. The `Build and Deploy Agent Image` workflow should complete with a green check. Confirm the image landed in ECR:

```bash
aws ecr describe-images \
  --repository-name backflow-agent \
  --region us-east-1 \
  --query 'imageDetails[0].imageTags'
```
