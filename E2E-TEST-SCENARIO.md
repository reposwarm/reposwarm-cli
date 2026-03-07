# E2E Test Scenario: Full RepoSwarm + Ask CLI Workflow

## Overview

This scenario tests the complete write/read pipeline:
1. **RepoSwarm** (write side): Install locally, configure provider, investigate a repo → generates `.arch.md` and pushes to arch-hub
2. **Ask CLI** (read side): Install, set up askbox, query architecture knowledge, browse results

## Prerequisites

- AWS EC2 instance (t4g.medium or larger, ARM64, Ubuntu 24.04)
- IAM role with `bedrock:InvokeModel` permission
- Docker + Docker Compose installed
- GitHub account with push access to an arch-hub repository
- `gh` CLI installed and authenticated

## Instance Setup

```bash
# Fresh Ubuntu instance — install Docker
sudo apt-get update && sudo apt-get install -y docker.io docker-compose-v2
sudo usermod -aG docker $USER
newgrp docker

# Install gh CLI
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list
sudo apt-get update && sudo apt-get install -y gh
gh auth login
```

## Step 1: Install RepoSwarm CLI

```bash
curl -fsSL https://raw.githubusercontent.com/reposwarm/reposwarm-cli/main/install.sh | sh
reposwarm version
```

**Expected:** Version string printed, binary at `/usr/local/bin/reposwarm`.

## Step 2: Local Setup

```bash
reposwarm new --local --for-agent --force
```

**Expected:**
- Docker containers start: temporal, postgres, dynamodb, api, ui, worker
- API healthy at `http://localhost:3000`
- UI at `http://localhost:3001`
- Temporal UI at `http://localhost:8233`
- API token generated and saved to `~/.reposwarm/config.json`

**Verify:**
```bash
reposwarm status --for-agent
docker ps --format '{{.Names}}\t{{.Status}}'
```

## Step 3: Configure LLM Provider

```bash
reposwarm config provider setup \
  --non-interactive \
  --provider bedrock \
  --region us-east-1 \
  --auth-method iam-role \
  --model sonnet \
  --pin
```

**Expected:**
- `~/.reposwarm/temporal/worker.env` written with Bedrock config
- Worker container restarted
- Inference health check passes

**Verify:**
```bash
reposwarm config provider show --for-agent
cat ~/.reposwarm/temporal/worker.env
```

### Known Issue: Model ID Resolution

The `--pin` flag may resolve model aliases to short IDs (e.g., `us.anthropic.claude-sonnet-4-6`) instead of full versioned IDs (e.g., `us.anthropic.claude-sonnet-4-20250514-v1:0`). If the worker logs show "No Bedrock mapping for..." warnings:

```bash
# Fix manually
sed -i 's|ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-6|ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-20250514-v1:0|' \
  ~/.reposwarm/temporal/worker.env
cd ~/.reposwarm/temporal && docker compose restart worker
```

## Step 4: Configure Arch-Hub

Create or use an existing GitHub repository for architecture results:

```bash
# Create arch-hub repo (one-time)
gh repo create <org>/e2e-arch-hub --private --description "Architecture hub"
```

Configure the worker to push results there:

```bash
# Set arch-hub in CLI config
reposwarm config set archHubUrl https://github.com/<org>/e2e-arch-hub.git

# Set worker env vars (compose .env for variable substitution)
cat >> ~/.reposwarm/temporal/.env << EOF
ARCH_HUB_BASE_URL=https://github.com/<org>
ARCH_HUB_REPO_NAME=e2e-arch-hub
GITHUB_TOKEN=$(gh auth token)
EOF

# Restart worker to pick up new env
cd ~/.reposwarm/temporal
docker compose stop worker && docker compose rm -f worker && docker compose up -d worker
```

### Known Issue: OAuth Token Format

The worker's `git_manager.py` uses `TOKEN@github.com` format for git auth, but `gho_` OAuth tokens (from `gh auth`) require `x-access-token:TOKEN@github.com`. If arch-hub push fails with "Authentication failed":

```bash
# Patch git_manager.py inside the container
docker exec reposwarm-worker sed -i \
  's|auth_netloc = f"{self.github_token}@{parsed.hostname}"|auth_netloc = f"x-access-token:{self.github_token}@{parsed.hostname}"|' \
  /app/src/investigator/core/git_manager.py

docker exec reposwarm-worker sed -i \
  "s|auth_url = current_url.replace('https://', f'https://{self.github_token}@')|auth_url = current_url.replace('https://', f'https://x-access-token:{self.github_token}@')|" \
  /app/src/investigator/core/git_manager.py

# Note: This patch is lost on container recreate. Use a classic PAT (ghp_) to avoid this issue.
```

**Best practice:** Use a GitHub classic Personal Access Token (`ghp_*`) with `repo` scope instead of `gho_*` OAuth tokens.

## Step 5: Add Repository and Investigate

```bash
reposwarm repos add is-odd --url https://github.com/jonschlinkert/is-odd --source GitHub --for-agent
reposwarm investigate is-odd --for-agent
```

**Expected:**
- Pre-flight checks pass
- Investigation starts (Temporal workflow)
- Worker clones repo, runs ~17 analysis steps via Bedrock
- Results saved to API wiki store
- `.arch.md` file pushed to arch-hub repository

**Monitor progress:**
```bash
# Watch workflow
reposwarm wf list --for-agent --json

# Watch worker logs
docker logs -f reposwarm-worker 2>&1 | grep -E 'step:|saved|push|ERROR'
```

**Verify results:**
```bash
reposwarm results list --for-agent
# Expected: is-odd with 17 sections

# Check arch-hub
git clone https://github.com/<org>/e2e-arch-hub.git /tmp/check-arch-hub
ls /tmp/check-arch-hub/
# Expected: is-odd.arch.md (2000+ lines)
```

**Typical duration:** 3-8 minutes (with cache: ~2 minutes).

## Step 6: Install Ask CLI

```bash
curl -fsSL https://raw.githubusercontent.com/reposwarm/ask/main/install.sh | sh
ask version
```

**Expected:** Version printed, binary at `/usr/local/bin/ask`.

## Step 7: Set Up Ask + Askbox

```bash
ask setup \
  --for-agent \
  --provider bedrock \
  --region us-east-1 \
  --auth iam-role \
  --model sonnet \
  --arch-hub https://github.com/<org>/e2e-arch-hub.git \
  --skip-docker
```

**Expected:**
- `~/.config/ask/config.json` created
- `~/.ask/askbox.env` created (mode 0600)
- `~/.ask/docker-compose.yml` created

**Auto-detection:** If RepoSwarm is installed, `ask setup` (without flags) auto-detects `~/.reposwarm/temporal/worker.env` and offers to reuse provider settings.

## Step 8: Start Askbox

```bash
ask up --for-agent
ask status --for-agent
```

**Expected:**
- Askbox container starts on port 8082
- Health check shows `ready` with 1+ repos loaded
- Arch-hub cloned automatically from configured URL

## Step 9: Query Architecture Knowledge

```bash
# AI-powered question
ask --for-agent "What does the is-odd library do and what are its dependencies?"
```

**Expected:**
- Answer returned in 20-60 seconds
- Uses tool calls to read arch-hub files
- Comprehensive answer about is-odd's purpose, dependencies, and architecture

```bash
# Browse results directly (no LLM, reads .arch.md files)
ask results list --path /tmp/arch-hub
ask results read is-odd --path /tmp/arch-hub | head -30
ask results search "dependencies" --path /tmp/arch-hub | head -10
ask results export is-odd --path /tmp/arch-hub -o /tmp/export.md
```

**Expected:**
- `list`: Shows 1 repo with sections
- `read`: Displays architecture content
- `search`: Finds matches across sections
- `export`: Writes markdown file

## Step 10: Docker Lifecycle

```bash
ask logs --for-agent          # View askbox logs
ask down --for-agent          # Stop askbox
ask status --for-agent        # Should show unhealthy/unreachable
ask up --for-agent            # Restart
ask status --for-agent        # Should show healthy again
```

## Cleanup

```bash
# Stop all containers
ask down
cd ~/.reposwarm/temporal && docker compose down -v

# Remove config
rm -rf ~/.reposwarm ~/.config/ask ~/.ask

# Terminate EC2 instance (if temporary)
# aws ec2 terminate-instances --instance-ids <id>
```

## Success Criteria

| Step | Criteria | Status |
|------|----------|--------|
| RepoSwarm install | Binary available, `reposwarm version` works | ☐ |
| Local setup | All 6 containers healthy | ☐ |
| Provider config | Worker env configured, inference works | ☐ |
| Investigation | 17 sections generated, `.arch.md` pushed to arch-hub | ☐ |
| Ask install | Binary available, `ask version` works | ☐ |
| Ask setup | Config + env + compose written | ☐ |
| Askbox | Container healthy, arch-hub loaded | ☐ |
| AI query | Answer returned with tool calls | ☐ |
| Results browse | list/read/search/export all work | ☐ |
| Docker lifecycle | up/down/logs/status all work | ☐ |

## E2E Test Run Log (2026-03-07)

**Instance:** i-071b54f2fb794ebb4 (t4g.medium, us-east-1, ARM64)
**Duration:** ~45 minutes (including debugging)
**Result:** ✅ All steps passed

Key findings:
- Model alias resolution needs fix (short vs versioned IDs)
- `gho_` OAuth tokens need `x-access-token:` prefix in worker git_manager.py
- Worker needs `ARCH_HUB_BASE_URL` + `ARCH_HUB_REPO_NAME` env vars (not `ARCH_HUB_URL`)
- `GITHUB_TOKEN` must be set in compose `.env` (not just `worker.env`) due to `${GITHUB_TOKEN:-}` in compose environment block
- Investigation uses prompt cache — re-runs are faster (~2 min vs ~8 min first run)
