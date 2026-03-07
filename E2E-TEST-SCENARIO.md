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
- Model resolves to full versioned ID: `us.anthropic.claude-sonnet-4-20250514-v1:0`
- Worker container restarted

**Verify:**
```bash
grep ANTHROPIC_MODEL ~/.reposwarm/temporal/worker.env
# Expected: ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-20250514-v1:0
```

## Step 4: Configure Arch-Hub + GitHub Token

Create or use an existing GitHub repository for architecture results:

```bash
# Create arch-hub repo (one-time)
gh repo create <org>/e2e-arch-hub --private --description "Architecture hub"
```

Configure the worker to push results there:

```bash
# Set arch-hub in CLI config
reposwarm config set archHubUrl https://github.com/<org>/e2e-arch-hub.git

# Add GitHub token + arch-hub env to worker.env
cat >> ~/.reposwarm/temporal/worker.env << EOF
GITHUB_TOKEN=$(gh auth token)
ARCH_HUB_BASE_URL=https://github.com/<org>
ARCH_HUB_REPO_NAME=e2e-arch-hub
EOF

# Restart worker to pick up new env
cd ~/.reposwarm/temporal
docker compose stop worker && docker compose rm -f worker && docker compose up -d worker
```

**Important:**
- Worker reads `ARCH_HUB_BASE_URL` + `ARCH_HUB_REPO_NAME` (NOT `ARCH_HUB_URL`)
- `GITHUB_TOKEN` must be in `worker.env` directly (compose env block no longer overrides it)
- Both OAuth tokens (`gho_*`) and classic PATs (`ghp_*`) work (worker uses `x-access-token:` prefix)

**Verify:**
```bash
docker exec reposwarm-worker env | grep -E 'GITHUB_TOKEN|ARCH_HUB' | head -c 50
```

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
- `~/.ask/askbox.env` created with:
  - `ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-20250514-v1:0` (full versioned ID, not alias)
  - `GITHUB_TOKEN=...` (auto-detected from RepoSwarm `worker.env`)
  - `ARCH_HUB_URL=https://github.com/<org>/e2e-arch-hub.git`
- `~/.ask/docker-compose.yml` created with host bind mount at `~/.ask/arch-hub`

**Auto-detection:** If RepoSwarm is installed, `ask setup` auto-detects `~/.reposwarm/temporal/worker.env` and picks up `GITHUB_TOKEN` and provider settings.

## Step 8: Start Askbox

```bash
ask up --for-agent
sleep 15  # wait for arch-hub clone
ask status --for-agent
```

**Expected:**
- Askbox container starts on port 8082
- Arch-hub cloned automatically (private repos use `GITHUB_TOKEN`)
- Health check shows `ready` with 1+ repos loaded
- Arch-hub files visible on host at `~/.ask/arch-hub/`

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
ask results list --path ~/.ask/arch-hub
ask results read is-odd --path ~/.ask/arch-hub | head -30
ask results search "dependencies" --path ~/.ask/arch-hub | head -10
ask results export is-odd --path ~/.ask/arch-hub -o /tmp/export.md
```

**Expected:**
- `list`: Shows 1 repo with section count
- `read`: Displays architecture content by section
- `search`: Finds matches across sections
- `export`: Writes full markdown file (1000+ lines)

**Note:** Both flat layout (`repo.arch.md` in root) and nested layout (`repo/repo.arch.md`) are supported.

## Step 10: Docker Lifecycle

```bash
ask logs --for-agent          # View askbox logs
ask down --for-agent          # Stop askbox
ask status --for-agent        # Should show unreachable (exit 1)
ask up --for-agent            # Restart
sleep 12
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

| Step | Criteria |
|------|----------|
| RepoSwarm install | Binary available, `reposwarm version` works |
| Local setup | All 6 containers healthy |
| Provider config | Model resolves to full versioned ID in worker.env |
| Arch-hub config | `GITHUB_TOKEN` + `ARCH_HUB_BASE_URL/REPO_NAME` set, worker pushes results |
| Investigation | 17 sections generated, `.arch.md` pushed to arch-hub |
| Ask install | Binary available, `ask version` works |
| Ask setup | Model resolved, GITHUB_TOKEN auto-detected, compose uses bind mount |
| Askbox | Container healthy, arch-hub cloned (private repo), files on host |
| AI query | Answer returned with tool calls |
| Results browse | list/read/search/export all work with flat arch-hub layout |
| Docker lifecycle | up/down/logs/status all work |

## E2E Test Runs

### Run 2: 2026-03-07 (post-fix)

**Instance:** i-071b54f2fb794ebb4 (t4g.medium, us-east-1, ARM64)
**Result:** ✅ All 11 steps passed

All bugs from Run 1 have been fixed:
- ✅ Model alias resolves to full versioned ID (`us.anthropic.claude-sonnet-4-20250514-v1:0`)
- ✅ Worker `git_manager.py` uses `x-access-token:TOKEN@` for all token types
- ✅ Compose no longer overrides `GITHUB_TOKEN` with empty string
- ✅ Askbox injects `GITHUB_TOKEN` for private arch-hub repos
- ✅ Ask results support flat arch-hub layout (files in root)
- ✅ Arch-hub uses host bind mount (accessible from host at `~/.ask/arch-hub`)
- ✅ `ask setup` auto-detects GITHUB_TOKEN from RepoSwarm worker.env

### Run 1: 2026-03-07 (initial)

**Result:** ⚠️ Passed with manual workarounds

Bugs found (all now fixed):
1. Model alias `--pin` resolved to short ID instead of full versioned ID
2. Worker `git_manager.py` used bare `TOKEN@` format (fails with `gho_*` OAuth tokens)
3. Compose `environment:` block `${GITHUB_TOKEN:-}` overrode worker.env value with empty string
4. Askbox had no `GITHUB_TOKEN` injection for private repo clones
5. Ask results only searched subdirectories, missed flat arch-hub layout
6. Arch-hub stored in Docker volume, inaccessible from host for `ask results`
