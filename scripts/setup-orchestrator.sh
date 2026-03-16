#!/usr/bin/env bash
set -euo pipefail

# Set up infrastructure for deploying the Backflow orchestrator to ECS Fargate.
# Creates: ECR repo, EFS filesystem, security group, IAM role, task definition, ECS service.
#
# Prerequisites:
#   - AWS CLI configured with appropriate permissions
#   - The base infrastructure from setup-aws.sh (ECS cluster, subnets, etc.)
#
# Usage:
#   bash scripts/setup-orchestrator.sh

REGION="${AWS_REGION:-us-east-1}"
ECS_CLUSTER="${BACKFLOW_ECS_CLUSTER:-backflow}"
ECR_REPO="backflow-orchestrator"
ORCH_SG_NAME="backflow-orchestrator-sg"
EFS_NAME="backflow-orchestrator-data"
ORCH_TASK_ROLE="backflow-orchestrator-task-role"
ORCH_CONTAINER_NAME="backflow-orchestrator"
CW_LOG_GROUP="${BACKFLOW_CLOUDWATCH_LOG_GROUP:-/ecs/backflow}"

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

echo "==> Setting up Backflow orchestrator infrastructure in ${REGION}"

# --- Discover VPC and subnets ---
VPC_ID=$(aws ec2 describe-vpcs \
    --filters "Name=isDefault,Values=true" \
    --query 'Vpcs[0].VpcId' \
    --output text \
    --region "$REGION")

SUBNET_IDS_RAW=$(aws ec2 describe-subnets \
    --filters "Name=vpc-id,Values=${VPC_ID}" \
    --query 'Subnets[*].SubnetId' \
    --output text \
    --region "$REGION")
SUBNET_IDS=$(echo "$SUBNET_IDS_RAW" | tr '\t' ',')
SUBNET_IDS_ARRAY=($SUBNET_IDS_RAW)

echo "    VPC: ${VPC_ID}"
echo "    Subnets: ${SUBNET_IDS}"

# --- ECR Repository ---
echo "==> Creating ECR repository for orchestrator..."
if aws ecr describe-repositories --repository-names "$ECR_REPO" --region "$REGION" &>/dev/null; then
    echo "    ECR repo already exists"
else
    aws ecr create-repository \
        --repository-name "$ECR_REPO" \
        --region "$REGION" \
        --image-scanning-configuration scanOnPush=true
fi

ECR_URI=$(aws ecr describe-repositories \
    --repository-names "$ECR_REPO" \
    --region "$REGION" \
    --query 'repositories[0].repositoryUri' \
    --output text)
echo "    ECR URI: ${ECR_URI}"

# --- Security Group ---
echo "==> Creating orchestrator security group..."
SG_ID=$(aws ec2 describe-security-groups \
    --filters "Name=group-name,Values=${ORCH_SG_NAME}" "Name=vpc-id,Values=${VPC_ID}" \
    --query 'SecurityGroups[0].GroupId' \
    --output text \
    --region "$REGION" 2>/dev/null) || true

if [ -z "$SG_ID" ] || [ "$SG_ID" = "None" ]; then
    SG_ID=$(aws ec2 create-security-group \
        --group-name "$ORCH_SG_NAME" \
        --description "Backflow orchestrator - API inbound, NFS, outbound" \
        --vpc-id "$VPC_ID" \
        --region "$REGION" \
        --query 'GroupId' \
        --output text)

    # Allow inbound on port 8080 (API)
    aws ec2 authorize-security-group-ingress \
        --group-id "$SG_ID" \
        --protocol tcp \
        --port 8080 \
        --cidr 0.0.0.0/0 \
        --region "$REGION"

    # Allow NFS (port 2049) within the security group for EFS
    aws ec2 authorize-security-group-ingress \
        --group-id "$SG_ID" \
        --protocol tcp \
        --port 2049 \
        --source-group "$SG_ID" \
        --region "$REGION"
else
    echo "    Security group already exists"
fi

echo "    Security group: ${SG_ID}"

# --- EFS Filesystem ---
echo "==> Creating EFS filesystem for SQLite persistence..."
EFS_ID=$(aws efs describe-file-systems \
    --region "$REGION" \
    --query "FileSystems[?Name=='${EFS_NAME}' && LifeCycleState=='available'].FileSystemId" \
    --output text 2>/dev/null) || true

if [ -z "$EFS_ID" ] || [ "$EFS_ID" = "None" ]; then
    EFS_ID=$(aws efs create-file-system \
        --performance-mode generalPurpose \
        --throughput-mode bursting \
        --encrypted \
        --tags "Key=Name,Value=${EFS_NAME}" \
        --region "$REGION" \
        --query 'FileSystemId' \
        --output text)

    echo "    Waiting for EFS filesystem to become available..."
    aws efs describe-file-systems \
        --file-system-id "$EFS_ID" \
        --region "$REGION" \
        --query 'FileSystems[0].LifeCycleState' \
        --output text
    sleep 5
else
    echo "    EFS filesystem already exists"
fi

echo "    EFS filesystem: ${EFS_ID}"

# Create mount targets in each subnet
echo "==> Creating EFS mount targets..."
for SUBNET in "${SUBNET_IDS_ARRAY[@]}"; do
    EXISTING_MT=$(aws efs describe-mount-targets \
        --file-system-id "$EFS_ID" \
        --region "$REGION" \
        --query "MountTargets[?SubnetId=='${SUBNET}'].MountTargetId" \
        --output text 2>/dev/null) || true
    if [ -z "$EXISTING_MT" ] || [ "$EXISTING_MT" = "None" ]; then
        aws efs create-mount-target \
            --file-system-id "$EFS_ID" \
            --subnet-id "$SUBNET" \
            --security-groups "$SG_ID" \
            --region "$REGION" >/dev/null
        echo "    Created mount target in ${SUBNET}"
    else
        echo "    Mount target already exists in ${SUBNET}"
    fi
done

# Create EFS access point
echo "==> Creating EFS access point..."
AP_ID=$(aws efs describe-access-points \
    --file-system-id "$EFS_ID" \
    --region "$REGION" \
    --query "AccessPoints[?RootDirectory.Path=='/backflow'].AccessPointId" \
    --output text 2>/dev/null) || true

if [ -z "$AP_ID" ] || [ "$AP_ID" = "None" ]; then
    AP_ID=$(aws efs create-access-point \
        --file-system-id "$EFS_ID" \
        --posix-user "Uid=1000,Gid=1000" \
        --root-directory "Path=/backflow,CreationInfo={OwnerUid=1000,OwnerGid=1000,Permissions=755}" \
        --region "$REGION" \
        --query 'AccessPointId' \
        --output text)
fi

echo "    Access point: ${AP_ID}"

# --- ECS Execution Role (reuse existing if available) ---
ECS_EXECUTION_ROLE="backflow-ecs-execution-role"
ECS_TRUST_POLICY=$(cat <<'ECSEOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"Service": "ecs-tasks.amazonaws.com"},
      "Action": "sts:AssumeRole"
    }
  ]
}
ECSEOF
)

if ! aws iam get-role --role-name "$ECS_EXECUTION_ROLE" &>/dev/null; then
    echo "==> Creating ECS execution role..."
    aws iam create-role \
        --role-name "$ECS_EXECUTION_ROLE" \
        --assume-role-policy-document "$ECS_TRUST_POLICY"
    aws iam attach-role-policy \
        --role-name "$ECS_EXECUTION_ROLE" \
        --policy-arn arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy
fi

# --- Orchestrator Task Role ---
echo "==> Creating orchestrator task role..."
if aws iam get-role --role-name "$ORCH_TASK_ROLE" &>/dev/null; then
    echo "    Task role already exists"
else
    aws iam create-role \
        --role-name "$ORCH_TASK_ROLE" \
        --assume-role-policy-document "$ECS_TRUST_POLICY"
fi

# Orchestrator needs permissions to manage ECS tasks, EC2 instances, SSM, CloudWatch, S3, EFS
ORCH_POLICY_NAME="backflow-orchestrator-policy"
ORCH_POLICY_ARN="arn:aws:iam::${ACCOUNT_ID}:policy/${ORCH_POLICY_NAME}"
S3_BUCKET="backflow-data-${ACCOUNT_ID}-${REGION}"

ORCH_POLICY=$(cat <<POLICYEOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ECSManagement",
      "Effect": "Allow",
      "Action": [
        "ecs:RunTask",
        "ecs:StopTask",
        "ecs:DescribeTasks",
        "ecs:DescribeTaskDefinition",
        "ecs:ListTasks"
      ],
      "Resource": "*"
    },
    {
      "Sid": "PassRoleForECS",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": [
        "arn:aws:iam::${ACCOUNT_ID}:role/${ECS_EXECUTION_ROLE}",
        "arn:aws:iam::${ACCOUNT_ID}:role/backflow-ecs-task-role"
      ]
    },
    {
      "Sid": "EC2Management",
      "Effect": "Allow",
      "Action": [
        "ec2:RunInstances",
        "ec2:TerminateInstances",
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus",
        "ec2:DescribeSpotInstanceRequests",
        "ec2:CreateFleet",
        "ec2:CreateTags"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SSMCommands",
      "Effect": "Allow",
      "Action": [
        "ssm:SendCommand",
        "ssm:GetCommandInvocation"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:GetLogEvents",
        "logs:FilterLogEvents"
      ],
      "Resource": "*"
    },
    {
      "Sid": "S3Access",
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject"],
      "Resource": [
        "arn:aws:s3:::${S3_BUCKET}/tasks/*",
        "arn:aws:s3:::${S3_BUCKET}/task-config/*"
      ]
    },
    {
      "Sid": "EFSAccess",
      "Effect": "Allow",
      "Action": [
        "elasticfilesystem:ClientMount",
        "elasticfilesystem:ClientWrite"
      ],
      "Resource": "arn:aws:elasticfilesystem:${REGION}:${ACCOUNT_ID}:file-system/${EFS_ID}"
    }
  ]
}
POLICYEOF
)

if aws iam get-policy --policy-arn "$ORCH_POLICY_ARN" &>/dev/null; then
    echo "    Policy already exists, creating new version..."
    aws iam create-policy-version \
        --policy-arn "$ORCH_POLICY_ARN" \
        --policy-document "$ORCH_POLICY" \
        --set-as-default
else
    aws iam create-policy \
        --policy-name "$ORCH_POLICY_NAME" \
        --policy-document "$ORCH_POLICY"
fi

aws iam attach-role-policy \
    --role-name "$ORCH_TASK_ROLE" \
    --policy-arn "$ORCH_POLICY_ARN"

echo "    Task role: ${ORCH_TASK_ROLE}"

# --- Ensure CloudWatch log group exists ---
echo "==> Ensuring CloudWatch log group exists..."
if ! aws logs describe-log-groups \
    --log-group-name-prefix "$CW_LOG_GROUP" \
    --region "$REGION" \
    --query "logGroups[?logGroupName=='${CW_LOG_GROUP}'].logGroupName" \
    --output text | grep -q "$CW_LOG_GROUP"; then
    aws logs create-log-group \
        --log-group-name "$CW_LOG_GROUP" \
        --region "$REGION"
    aws logs put-retention-policy \
        --log-group-name "$CW_LOG_GROUP" \
        --retention-in-days 14 \
        --region "$REGION"
fi

# --- ECS Task Definition ---
echo "==> Registering orchestrator ECS task definition..."
TASK_DEF=$(cat <<TDEOF
{
  "family": "${ORCH_CONTAINER_NAME}",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "512",
  "memory": "1024",
  "executionRoleArn": "arn:aws:iam::${ACCOUNT_ID}:role/${ECS_EXECUTION_ROLE}",
  "taskRoleArn": "arn:aws:iam::${ACCOUNT_ID}:role/${ORCH_TASK_ROLE}",
  "volumes": [
    {
      "name": "backflow-data",
      "efsVolumeConfiguration": {
        "fileSystemId": "${EFS_ID}",
        "transitEncryption": "ENABLED",
        "authorizationConfig": {
          "accessPointId": "${AP_ID}",
          "iam": "ENABLED"
        }
      }
    }
  ],
  "containerDefinitions": [
    {
      "name": "${ORCH_CONTAINER_NAME}",
      "image": "${ECR_URI}:latest",
      "essential": true,
      "portMappings": [
        {
          "containerPort": 8080,
          "protocol": "tcp"
        }
      ],
      "mountPoints": [
        {
          "sourceVolume": "backflow-data",
          "containerPath": "/data"
        }
      ],
      "environment": [
        {"name": "BACKFLOW_DB_PATH", "value": "/data/backflow.db"},
        {"name": "BACKFLOW_LISTEN_ADDR", "value": ":8080"},
        {"name": "AWS_REGION", "value": "${REGION}"},
        {"name": "BACKFLOW_MODE", "value": "fargate"},
        {"name": "BACKFLOW_ECS_CLUSTER", "value": "${ECS_CLUSTER}"},
        {"name": "BACKFLOW_ECS_TASK_DEFINITION", "value": "backflow-agent"},
        {"name": "BACKFLOW_ECS_SUBNETS", "value": "${SUBNET_IDS}"},
        {"name": "BACKFLOW_ECS_SECURITY_GROUPS", "value": "${SG_ID}"},
        {"name": "BACKFLOW_CLOUDWATCH_LOG_GROUP", "value": "${CW_LOG_GROUP}"},
        {"name": "BACKFLOW_S3_BUCKET", "value": "${S3_BUCKET}"}
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "${CW_LOG_GROUP}",
          "awslogs-region": "${REGION}",
          "awslogs-stream-prefix": "orchestrator"
        }
      },
      "healthCheck": {
        "command": ["CMD-SHELL", "curl -f http://localhost:8080/api/v1/health || exit 1"],
        "interval": 30,
        "timeout": 5,
        "retries": 3,
        "startPeriod": 10
      }
    }
  ]
}
TDEOF
)

TASK_DEF_ARN=$(aws ecs register-task-definition \
    --cli-input-json "$TASK_DEF" \
    --region "$REGION" \
    --query 'taskDefinition.taskDefinitionArn' \
    --output text)
echo "    Task definition: ${TASK_DEF_ARN}"

# --- ECS Service ---
echo "==> Creating ECS service..."
if aws ecs describe-services \
    --cluster "$ECS_CLUSTER" \
    --services "$ORCH_CONTAINER_NAME" \
    --region "$REGION" \
    --query "services[?status=='ACTIVE'].serviceName" \
    --output text 2>/dev/null | grep -q "$ORCH_CONTAINER_NAME"; then
    echo "    Service already exists, updating..."
    aws ecs update-service \
        --cluster "$ECS_CLUSTER" \
        --service "$ORCH_CONTAINER_NAME" \
        --task-definition "$ORCH_CONTAINER_NAME" \
        --force-new-deployment \
        --region "$REGION" >/dev/null
else
    aws ecs create-service \
        --cluster "$ECS_CLUSTER" \
        --service-name "$ORCH_CONTAINER_NAME" \
        --task-definition "$ORCH_CONTAINER_NAME" \
        --desired-count 1 \
        --launch-type FARGATE \
        --platform-version LATEST \
        --network-configuration "awsvpcConfiguration={subnets=[$(echo "$SUBNET_IDS" | sed 's/,/,/g')],securityGroups=[${SG_ID}],assignPublicIp=ENABLED}" \
        --deployment-configuration "maximumPercent=200,minimumHealthyPercent=100" \
        --region "$REGION" >/dev/null
fi

echo "    Service: ${ORCH_CONTAINER_NAME}"

echo ""
echo "==> Orchestrator infrastructure setup complete!"
echo ""
echo "Resources created:"
echo "  ECR:             ${ECR_URI}"
echo "  EFS:             ${EFS_ID} (access point: ${AP_ID})"
echo "  Security group:  ${SG_ID}"
echo "  Task role:       ${ORCH_TASK_ROLE}"
echo "  Task definition: ${ORCH_CONTAINER_NAME}"
echo "  ECS service:     ${ORCH_CONTAINER_NAME} (cluster: ${ECS_CLUSTER})"
echo ""
echo "Next steps:"
echo "  1. Set secrets (ANTHROPIC_API_KEY, GITHUB_TOKEN) in the task definition"
echo "     via the AWS console or by updating the task def with 'secrets' entries"
echo "     referencing SSM Parameter Store or Secrets Manager."
echo "  2. Build and deploy: bash scripts/deploy-orchestrator.sh"
echo ""
echo "To set secrets via SSM Parameter Store:"
echo "  aws ssm put-parameter --name /backflow/ANTHROPIC_API_KEY --value 'sk-ant-...' --type SecureString"
echo "  aws ssm put-parameter --name /backflow/GITHUB_TOKEN --value 'ghp_...' --type SecureString"
echo "  Then add 'secrets' to the container definition referencing these parameters."
