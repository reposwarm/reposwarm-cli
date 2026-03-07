# E2E Test Scenario: RepoSwarm + Ask CLI

Full end-to-end workflow testing the write/read split between RepoSwarm (investigations) and the `ask` CLI (querying architecture knowledge).

## Prerequisites

- Fresh EC2 instance (tested on t4g.medium, Ubuntu 24.04, ARM64)
- IAM role with Bedrock `InvokeModel` permission (e.g., `AllAccessAdmin`)
- Docker + Docker Compose v2 installed
- SSH access from test runner

## Instance Setup

```bash
# If reusing an existing instance, clean up first:
docker stop $(docker ps -aq) && docker rm $(docker ps -aq)
docker volume prune -f && docker network prune -f
docker rmi $(docker images -q)
rm -rf ~/.reposwarm ~/.config/ask ~/.ask /tmp/arch-hub
```

## Test Steps

### Step 1: Install CLIs

```bash
# Install reposwarm CLI (from release or local build)
curl -fsSL https://raw.githubusercontent.com/reposwarm/reposwarm-cli/main/install.sh | sh

# Install ask CLI
curl -fsSL https://raw.githubusercontent.com/reposwarm/ask/main/install.sh | sh

# Verify
reposwarm version
ask version
```

### Step 2: Set up RepoSwarm (local Docker mode)

```bash
reposwarm new --local --for-agent --force
```

**Expected:** Temporal, DynamoDB, API, UI containers start. CLI configured with API token.

**Verify:**
```bash
reposwarm status --for-agent
# Should show: temporal=healthy, dynamodb=healthy, api=healthy, ui=healthy
```

### Step 3: Configure LLM provider

```bash
reposwarm config provider setup \
  --for-agent \
  --non-interactive \
  --provider bedrock \
  --region us-east-1 \
  --auth-method iam-role \
  --model sonnet \
  --pin
```

**⚠️ Known issue:** Model alias resolution may produce incomplete model IDs. Verify and fix if needed:
```bash
cat ~/.reposwarm/temporal/worker.env | grep ANTHROPIC_MODEL
# Should be: ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-20250514-v1:0
# If not, fix manually:
sed -i 's|ANTHROPIC_MODEL=.*|ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-20250514-v1:0|' ~/.reposwarm/temporal/worker.env
```

Also set up an arch-hub directory and add it to worker env:
```bash
# Create local arch-hub
mkdir -p /tmp/arch-hub && cd /tmp/arch-hub
git init && git config user.email 'test@test.com' && git config user.name 'test'
echo '# Architecture Hub' > README.md && git add . && git commit -m 'init'

# Add to worker env
echo 'ARCH_HUB_URL=/tmp/arch-hub' >> ~/.reposwarm/temporal/worker.env

# Mount arch-hub into worker container (add to compose volumes)
# In ~/.reposwarm/temporal/docker-compose.yml, under worker.volumes, add:
#   - /tmp/arch-hub:/tmp/arch-hub
```

Restart worker:
```bash
cd ~/.reposwarm/temporal
docker compose stop worker && docker compose rm -f worker && docker compose up -d worker
```

### Step 4: Add a repo and investigate

```bash
reposwarm repos add is-odd --url https://github.com/jonschlinkert/is-odd --source GitHub --for-agent
reposwarm investigate is-odd --for-agent
```

**Wait for completion** (2-5 min depending on cache):
```bash
# Poll workflow status
reposwarm wf list --for-agent --json
# Wait until status = "Completed"
```

**Verify results:**
```bash
reposwarm results list --for-agent
# Expected: is-odd with 17 sections

reposwarm results sections is-odd --for-agent
# Expected: hl_overview, module_deep_dive, dependencies, core_entities, DBs, APIs, events, etc.
```

### Step 5: Export results for ask CLI

```bash
reposwarm results export is-odd -d /tmp/arch-hub/is-odd --for-agent
# Expected: ~75KB .arch.md file
```

### Step 6: Set up ask CLI

```bash
ask setup \
  --for-agent \
  --provider bedrock \
  --region us-east-1 \
  --model sonnet \
  --arch-hub /tmp/arch-hub \
  --non-interactive \
  --skip-docker
```

**Fix env file model ID** (if alias not fully resolved):
```bash
sed -i 's|ANTHROPIC_MODEL=sonnet|ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-20250514-v1:0|' ~/.ask/askbox.env
```

**Fix compose for local arch-hub** (bind mount instead of Docker volume):
```bash
cat > ~/.ask/docker-compose.yml << 'EOF'
services:
  askbox:
    container_name: askbox
    image: ghcr.io/reposwarm/askbox:latest
    network_mode: host
    env_file:
      - askbox.env
    environment:
      - ASKBOX_PORT=8082
    volumes:
      - /tmp/arch-hub:/tmp/arch-hub
    healthcheck:
      test: ["CMD", "python3", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:8082/health')"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped
EOF
```

### Step 7: Start askbox and query

```bash
ask up --for-agent
# Wait 10s for healthcheck

ask status --for-agent
# Expected: healthy, arch-hub ready (1 repos)
```

#### 7a: Browse results locally

```bash
ask results list --path /tmp/arch-hub --for-agent
# Expected: is-odd with sections

ask results search 'dependencies' --path /tmp/arch-hub --for-agent --max 5
# Expected: matches in is-odd arch docs
```

#### 7b: Ask questions via askbox

```bash
ask --for-agent 'what does the is-odd package do and what are its main dependencies?'
# Expected: detailed answer about is-odd functionality and deps (30-60s)

ask --for-agent 'are there any security concerns with this package?'
# Expected: security analysis based on arch docs (30-60s)
```

#### 7c: Check job history

```bash
ask list --for-agent
# Expected: 2 completed jobs
```

#### 7d: Export results

```bash
ask results export is-odd --path /tmp/arch-hub -o /tmp/exported.arch.md --for-agent
# Expected: exported markdown file
```

### Step 8: Cleanup

```bash
ask down --for-agent
# Expected: askbox container stopped and removed

# Stop reposwarm
cd ~/.reposwarm/temporal && docker compose down

# Verify clean
docker ps -a
# Expected: no containers running
```

## Success Criteria

| Step | Criterion |
|------|-----------|
| 1 | Both CLIs install and report version |
| 2 | RepoSwarm local setup completes, all 4 services healthy |
| 3 | Provider configured, worker env has correct model ID |
| 4 | Investigation completes with 17 sections for is-odd |
| 5 | Results exported to local .arch.md file |
| 6 | ask setup creates config.json, askbox.env, docker-compose.yml |
| 7a | `ask results list` shows repos from local arch-hub |
| 7b | Two questions answered via askbox (both completed) |
| 7c | `ask list` shows 2 completed jobs |
| 7d | `ask results export` produces valid markdown file |
| 8 | All containers stopped cleanly |

## Known Issues

1. **Model alias resolution:** `--model sonnet --pin` may produce `us.anthropic.claude-sonnet-4-6` instead of the full `us.anthropic.claude-sonnet-4-20250514-v1:0`. Manual fix needed in worker.env and askbox.env.
2. **Arch-hub save activity:** Worker tries to `git clone` the arch-hub URL. Local filesystem paths work only with bind mounts. For production, use a GitHub/GitLab repo URL.
3. **ask setup compose template:** Uses Docker volume for arch-hub by default. For local paths, replace with bind mount manually.

## Tested On

- **Date:** 2026-03-07
- **Instance:** i-071b54f2fb794ebb4 (t4g.medium, Ubuntu 24.04 ARM64)
- **Region:** us-east-1
- **RepoSwarm CLI:** commit 95700e9
- **Ask CLI:** commit 3d957cd
- **Askbox:** ghcr.io/reposwarm/askbox:latest
- **LLM:** Bedrock Claude Sonnet 4 (us.anthropic.claude-sonnet-4-20250514-v1:0)
