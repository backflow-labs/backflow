#!/usr/bin/env bash
set -euo pipefail

# Build, push, and deploy the Backflow orchestrator to ECS Fargate.
#
# Prerequisites:
#   - Infrastructure created via setup-orchestrator.sh
#   - AWS CLI configured with appropriate permissions
#   - Docker with buildx support
#
# Usage:
#   bash scripts/deploy-orchestrator.sh [--skip-build]

REGION="${AWS_REGION:-us-east-1}"
ECR_REPO="backflow-orchestrator"
ECS_CLUSTER="${BACKFLOW_ECS_CLUSTER:-backflow}"
ECS_SERVICE="backflow-orchestrator"
SKIP_BUILD=false

for arg in "$@"; do
    case $arg in
        --skip-build) SKIP_BUILD=true ;;
        *) echo "Unknown argument: $arg" >&2; exit 1 ;;
    esac
done

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
ECR_URI="${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com/${ECR_REPO}"

if [ "$SKIP_BUILD" = false ]; then
    echo "==> Authenticating with ECR..."
    aws ecr get-login-password --region "$REGION" \
        | docker login --username AWS --password-stdin "${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com"

    echo "==> Building orchestrator image..."
    docker buildx create --name backflow-builder --use 2>/dev/null || docker buildx use backflow-builder

    GIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        -t "${ECR_URI}:latest" \
        -t "${ECR_URI}:${GIT_SHA}" \
        --push \
        .

    echo "==> Pushed ${ECR_URI}:latest and ${ECR_URI}:${GIT_SHA}"
fi

echo "==> Deploying to ECS..."
aws ecs update-service \
    --cluster "$ECS_CLUSTER" \
    --service "$ECS_SERVICE" \
    --force-new-deployment \
    --region "$REGION" >/dev/null

echo "==> Waiting for deployment to stabilize..."
aws ecs wait services-stable \
    --cluster "$ECS_CLUSTER" \
    --services "$ECS_SERVICE" \
    --region "$REGION" 2>/dev/null || echo "    (timed out waiting — check ECS console for status)"

echo "==> Deployment complete!"

# Show the running task's public IP if available
TASK_ARN=$(aws ecs list-tasks \
    --cluster "$ECS_CLUSTER" \
    --service-name "$ECS_SERVICE" \
    --desired-status RUNNING \
    --region "$REGION" \
    --query 'taskArns[0]' \
    --output text 2>/dev/null) || true

if [ -n "$TASK_ARN" ] && [ "$TASK_ARN" != "None" ]; then
    ENI_ID=$(aws ecs describe-tasks \
        --cluster "$ECS_CLUSTER" \
        --tasks "$TASK_ARN" \
        --region "$REGION" \
        --query 'tasks[0].attachments[0].details[?name==`networkInterfaceId`].value' \
        --output text 2>/dev/null) || true

    if [ -n "$ENI_ID" ] && [ "$ENI_ID" != "None" ]; then
        PUBLIC_IP=$(aws ec2 describe-network-interfaces \
            --network-interface-ids "$ENI_ID" \
            --region "$REGION" \
            --query 'NetworkInterfaces[0].Association.PublicIp' \
            --output text 2>/dev/null) || true

        if [ -n "$PUBLIC_IP" ] && [ "$PUBLIC_IP" != "None" ]; then
            echo ""
            echo "Orchestrator API: http://${PUBLIC_IP}:8080"
            echo "Health check:     http://${PUBLIC_IP}:8080/api/v1/health"
        fi
    fi
fi
