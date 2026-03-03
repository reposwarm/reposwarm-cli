#!/usr/bin/env bash
# Launch a clean EC2 instance, deploy RepoSwarm, and run E2E provider tests
# Usage: ./run-e2e-clean-instance.sh [--keep] [--scenario 13]
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'; BOLD='\033[1m'

# ── Config ──
INSTANCE_TYPE="t4g.medium"
AMI=""  # Will auto-detect latest Ubuntu 24.04 ARM64
REGION="${AWS_REGION:-us-east-1}"
KEY_NAME="reposwarm-e2e"
SG_NAME="reposwarm-e2e-sg"
KEEP_INSTANCE=false
SCENARIO="13"
SUBNET_ID=""  # Will auto-detect

# ── Parse args ──
while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep) KEEP_INSTANCE=true; shift ;;
    --scenario) SCENARIO="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

echo -e "${BOLD}${CYAN}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${CYAN}║  RepoSwarm E2E — Clean Instance Test Runner  ║${NC}"
echo -e "${BOLD}${CYAN}╚══════════════════════════════════════════════╝${NC}"

# ── Step 1: Build the E2E bundle ──
echo -e "\n${BOLD}Step 1: Build E2E bundle${NC}"

BUNDLE_DIR=$(mktemp -d)
trap "rm -rf $BUNDLE_DIR" EXIT

# Build CLI binary (linux arm64)
echo "  Building CLI binary..."
cd /tmp/reposwarm-cli
export PATH=$PATH:/usr/local/go/bin
GOOS=linux GOARCH=arm64 go build -o "$BUNDLE_DIR/reposwarm" ./cmd/reposwarm 2>&1

# Copy test scenarios
cp -r /tmp/reposwarm-cli/test-scenarios "$BUNDLE_DIR/"

# Copy API dist
mkdir -p "$BUNDLE_DIR/api"
cp -r /tmp/reposwarm-api/dist "$BUNDLE_DIR/api/"
cp /tmp/reposwarm-api/package.json "$BUNDLE_DIR/api/"
cp /tmp/reposwarm-api/package-lock.json "$BUNDLE_DIR/api/"

# Create bootstrap script
cat > "$BUNDLE_DIR/bootstrap.sh" << 'BOOTSTRAP'
#!/usr/bin/env bash
set -euo pipefail

echo "=== RepoSwarm E2E Bootstrap ==="

# Install deps
sudo apt-get update -qq
sudo apt-get install -y -qq docker.io docker-compose-v2 nodejs npm python3 python3-venv jq curl > /dev/null

# Start Docker
sudo systemctl start docker
sudo usermod -aG docker ubuntu

# Install the CLI
sudo cp /home/ubuntu/e2e/reposwarm /usr/local/bin/reposwarm
chmod +x /usr/local/bin/reposwarm

# Create minimal config
mkdir -p /home/ubuntu/.reposwarm
API_TOKEN=$(openssl rand -hex 16)
cat > /home/ubuntu/.reposwarm/config.json << EOF
{
  "apiUrl": "http://localhost:3000/v1",
  "apiToken": "$API_TOKEN",
  "region": "us-east-1",
  "defaultModel": "us.anthropic.claude-sonnet-4-6",
  "providerConfig": {
    "provider": "bedrock",
    "awsRegion": "us-east-1",
    "bedrockAuth": "iam-role"
  }
}
EOF

# Start Temporal via Docker Compose
echo "=== Starting Temporal ==="
mkdir -p /home/ubuntu/reposwarm/temporal
cat > /home/ubuntu/reposwarm/temporal/docker-compose.yml << 'COMPOSE'
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: temporal
      POSTGRES_PASSWORD: temporal
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U temporal"]
      interval: 5s
      timeout: 5s
      retries: 10
    volumes:
      - temporal-data:/var/lib/postgresql/data

  temporal:
    image: temporalio/auto-setup:latest
    ports:
      - "7233:7233"
    environment:
      DB: postgres12
      POSTGRES_USER: temporal
      POSTGRES_PWD: temporal
      POSTGRES_SEEDS: postgres
    depends_on:
      postgres:
        condition: service_healthy

  temporal-ui:
    image: temporalio/ui:latest
    ports:
      - "8233:8080"
    environment:
      TEMPORAL_ADDRESS: temporal:7233
    depends_on:
      - temporal

volumes:
  temporal-data:
COMPOSE

cd /home/ubuntu/reposwarm/temporal
sudo docker compose up -d 2>&1

# Wait for Temporal
echo "Waiting for Temporal..."
for i in $(seq 1 60); do
  if curl -sf http://localhost:7233/api/v1/namespaces 2>/dev/null | grep -q "default"; then
    echo "Temporal ready (${i}s)"
    break
  fi
  sleep 2
done

# Start API server
echo "=== Starting API Server ==="
mkdir -p /home/ubuntu/reposwarm/worker
cat > /home/ubuntu/reposwarm/worker/.env << EOF2
CLAUDE_CODE_USE_BEDROCK=1
AWS_REGION=us-east-1
ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-6
EOF2

cd /home/ubuntu/e2e/api
npm install --production 2>&1 | tail -1

export PORT=3000
export TEMPORAL_SERVER_URL=localhost:7233
export TEMPORAL_HTTP_URL=http://localhost:8233
export TEMPORAL_NAMESPACE=default
export TEMPORAL_TASK_QUEUE=investigate-task-queue
export AWS_REGION=us-east-1
export DYNAMODB_TABLE=reposwarm-cache
export API_BEARER_TOKEN=$API_TOKEN
export REPOSWARM_INSTALL_DIR=/home/ubuntu/reposwarm

# Create start-api.sh for auto-restart
cat > /tmp/start-api.sh << APISTART
#!/usr/bin/env bash
cd /home/ubuntu/e2e/api
PORT=3000 TEMPORAL_SERVER_URL=localhost:7233 TEMPORAL_NAMESPACE=default \
TEMPORAL_TASK_QUEUE=investigate-task-queue AWS_REGION=us-east-1 \
DYNAMODB_TABLE=reposwarm-cache API_BEARER_TOKEN=$API_TOKEN \
REPOSWARM_INSTALL_DIR=/home/ubuntu/reposwarm \
nohup node dist/index.js > /home/ubuntu/reposwarm/api.log 2>&1 &
APISTART
chmod +x /tmp/start-api.sh

nohup node dist/index.js > /home/ubuntu/reposwarm/api.log 2>&1 &

echo "Waiting for API..."
for i in $(seq 1 30); do
  if curl -sf http://localhost:3000/v1/health >/dev/null 2>&1; then
    echo "API ready (${i}s)"
    break
  fi
  sleep 2
done

echo "=== Bootstrap complete ==="
BOOTSTRAP

chmod +x "$BUNDLE_DIR/bootstrap.sh"

# Create tarball
BUNDLE_TAR="/tmp/reposwarm-e2e-provider-bundle.tar.gz"
cd "$BUNDLE_DIR" && tar czf "$BUNDLE_TAR" .
echo -e "  ${GREEN}✓${NC} Bundle: $BUNDLE_TAR ($(du -h "$BUNDLE_TAR" | cut -f1))"

# ── Step 2: Find/create SSH key ──
echo -e "\n${BOLD}Step 2: SSH key${NC}"
KEY_FILE="$HOME/.ssh/${KEY_NAME}.pem"
if [ ! -f "$KEY_FILE" ]; then
  echo "  Creating EC2 key pair..."
  aws ec2 create-key-pair --key-name "$KEY_NAME" --region "$REGION" \
    --query 'KeyMaterial' --output text > "$KEY_FILE" 2>/dev/null || true
  chmod 600 "$KEY_FILE"
fi
echo -e "  ${GREEN}✓${NC} Key: $KEY_FILE"

# ── Step 3: Find/create security group ──
echo -e "\n${BOLD}Step 3: Security group${NC}"
SG_ID=$(aws ec2 describe-security-groups --region "$REGION" \
  --filters "Name=group-name,Values=$SG_NAME" \
  --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null)

if [ "$SG_ID" = "None" ] || [ -z "$SG_ID" ]; then
  # Get default VPC
  VPC_ID=$(aws ec2 describe-vpcs --region "$REGION" --filters "Name=isDefault,Values=true" \
    --query 'Vpcs[0].VpcId' --output text 2>/dev/null)
  if [ "$VPC_ID" = "None" ] || [ -z "$VPC_ID" ]; then
    # Use OpenClaw VPC
    VPC_ID=$(aws ec2 describe-vpcs --region "$REGION" \
      --filters "Name=tag:Name,Values=openclaw-master-vpc" \
      --query 'Vpcs[0].VpcId' --output text 2>/dev/null)
  fi

  SG_ID=$(aws ec2 create-security-group --group-name "$SG_NAME" \
    --description "RepoSwarm E2E tests" --vpc-id "$VPC_ID" --region "$REGION" \
    --query 'GroupId' --output text)
  aws ec2 authorize-security-group-ingress --group-id "$SG_ID" --region "$REGION" \
    --protocol tcp --port 22 --cidr 10.0.0.0/16 2>/dev/null || true
fi
echo -e "  ${GREEN}✓${NC} SG: $SG_ID"

# ── Step 4: Find AMI ──
echo -e "\n${BOLD}Step 4: AMI${NC}"
if [ -z "$AMI" ]; then
  AMI=$(aws ec2 describe-images --region "$REGION" --owners 099720109477 \
    --filters "Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-arm64-server-*" \
              "Name=architecture,Values=arm64" \
              "Name=state,Values=available" \
    --query 'sort_by(Images, &CreationDate)[-1].ImageId' --output text)
fi
echo -e "  ${GREEN}✓${NC} AMI: $AMI"

# ── Step 5: Find subnet ──
echo -e "\n${BOLD}Step 5: Subnet${NC}"
if [ -z "$SUBNET_ID" ]; then
  # Use a public subnet in OpenClaw VPC
  SUBNET_ID=$(aws ec2 describe-subnets --region "$REGION" \
    --filters "Name=tag:Name,Values=*openclaw*public*" \
    --query 'Subnets[0].SubnetId' --output text 2>/dev/null)
  if [ "$SUBNET_ID" = "None" ] || [ -z "$SUBNET_ID" ]; then
    SUBNET_ID=$(aws ec2 describe-subnets --region "$REGION" \
      --filters "Name=default-for-az,Values=true" \
      --query 'Subnets[0].SubnetId' --output text 2>/dev/null)
  fi
fi
echo -e "  ${GREEN}✓${NC} Subnet: $SUBNET_ID"

# ── Step 6: Launch instance ──
echo -e "\n${BOLD}Step 6: Launch EC2 instance${NC}"

# Get IAM instance profile (same as OpenClaw instance for Bedrock access)
PROFILE_ARN=$(aws ec2 describe-instances --region "$REGION" \
  --instance-ids i-053b8c1998c21cdb6 \
  --query 'Reservations[0].Instances[0].IamInstanceProfile.Arn' --output text 2>/dev/null || echo "")
PROFILE_NAME=""
if [ -n "$PROFILE_ARN" ] && [ "$PROFILE_ARN" != "None" ]; then
  PROFILE_NAME=$(echo "$PROFILE_ARN" | sed 's|.*/||')
fi

LAUNCH_CMD="aws ec2 run-instances --region $REGION \
  --image-id $AMI \
  --instance-type $INSTANCE_TYPE \
  --key-name $KEY_NAME \
  --security-group-ids $SG_ID \
  --subnet-id $SUBNET_ID \
  --associate-public-ip-address \
  --block-device-mappings '[{\"DeviceName\":\"/dev/sda1\",\"Ebs\":{\"VolumeSize\":20,\"VolumeType\":\"gp3\"}}]' \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=reposwarm-e2e-test},{Key=Purpose,Value=e2e-test},{Key=AutoTerminate,Value=true}]'"

if [ -n "$PROFILE_NAME" ]; then
  LAUNCH_CMD="$LAUNCH_CMD --iam-instance-profile Name=$PROFILE_NAME"
fi

INSTANCE_ID=$(eval $LAUNCH_CMD --query 'Instances[0].InstanceId' --output text 2>&1)
echo -e "  ${GREEN}✓${NC} Instance: $INSTANCE_ID"

echo "  Waiting for running state..."
aws ec2 wait instance-running --instance-ids "$INSTANCE_ID" --region "$REGION"

INSTANCE_IP=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" --region "$REGION" \
  --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text)
echo -e "  ${GREEN}✓${NC} IP: $INSTANCE_IP"

# Cleanup on exit (unless --keep)
if [ "$KEEP_INSTANCE" = false ]; then
  trap "echo 'Terminating $INSTANCE_ID...'; aws ec2 terminate-instances --instance-ids $INSTANCE_ID --region $REGION >/dev/null 2>&1; rm -rf $BUNDLE_DIR" EXIT
fi

# ── Step 7: Wait for SSH ──
echo -e "\n${BOLD}Step 7: Wait for SSH${NC}"
SSH="ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -i $KEY_FILE ubuntu@$INSTANCE_IP"
SCP="scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i $KEY_FILE"

for i in $(seq 1 60); do
  if $SSH "echo ok" 2>/dev/null | grep -q ok; then
    echo -e "  ${GREEN}✓${NC} SSH ready (${i}s)"
    break
  fi
  sleep 3
done

# ── Step 8: Upload bundle ──
echo -e "\n${BOLD}Step 8: Upload bundle${NC}"
$SCP "$BUNDLE_TAR" "ubuntu@$INSTANCE_IP:/tmp/e2e-bundle.tar.gz" 2>/dev/null
$SSH "mkdir -p /home/ubuntu/e2e && cd /home/ubuntu/e2e && tar xzf /tmp/e2e-bundle.tar.gz" 2>/dev/null
echo -e "  ${GREEN}✓${NC} Bundle uploaded and extracted"

# ── Step 9: Bootstrap ──
echo -e "\n${BOLD}Step 9: Bootstrap stack${NC}"
$SSH "cd /home/ubuntu/e2e && bash bootstrap.sh" 2>&1

# ── Step 10: Run E2E tests ──
echo -e "\n${BOLD}Step 10: Run E2E scenario ${SCENARIO}${NC}"
$SSH "export CLI=/usr/local/bin/reposwarm && \
  export API_URL=http://localhost:3000/v1 && \
  export API_TOKEN=\$(cat /home/ubuntu/.reposwarm/config.json | python3 -c 'import sys,json; print(json.load(sys.stdin)[\"apiToken\"])') && \
  cd /home/ubuntu/e2e/test-scenarios && \
  bash ${SCENARIO}-*.sh" 2>&1

EXIT_CODE=${PIPESTATUS[0]:-$?}

# ── Step 11: Collect results ──
echo -e "\n${BOLD}Step 11: Results${NC}"
$SSH "cat /tmp/reposwarm-test-*.log 2>/dev/null | tail -20" 2>/dev/null || true

if [ "$KEEP_INSTANCE" = true ]; then
  echo -e "\n${YELLOW}Instance kept: $INSTANCE_ID ($INSTANCE_IP)${NC}"
  echo -e "  SSH: ssh -i $KEY_FILE ubuntu@$INSTANCE_IP"
fi

exit $EXIT_CODE
