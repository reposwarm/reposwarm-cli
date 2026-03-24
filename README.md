# reposwarm-cli

CLI for [RepoSwarm](https://github.com/reposwarm/reposwarm) — AI-powered multi-repo architecture discovery.

Written in Go. Single 9MB binary, zero runtime dependencies, 4ms startup.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/reposwarm/reposwarm-cli/main/install.sh | sh
```

Or build from source:
```bash
git clone https://github.com/reposwarm/reposwarm-cli.git
cd reposwarm-cli
go build -o reposwarm ./cmd/reposwarm
```

---

## 🤖 Agent-Native

Every command supports `--for-agent` for machine-friendly output. No spinners, no colors, no interactive prompts — just clean text that AI agents can parse.

```bash
# Agent gets JSON output
reposwarm status --for-agent --json

# Full local setup — zero human interaction needed
reposwarm new --local --for-agent

# Configure provider non-interactively
reposwarm config provider setup --provider bedrock --auth-method iam-role --region us-east-1 --non-interactive

# Debug logs (truncated at 5000 chars, with path to full file)
reposwarm debug-logs --for-agent

# Investigate and get results
reposwarm investigate https://github.com/org/repo --for-agent
reposwarm results read my-repo --for-agent
```

Designed for [OpenClaw](https://openclaw.ai), Claude Code, Codex, and any AI coding agent.

---

## Prerequisites

For **local setup** (`reposwarm new --local`):

| Tool | Version | Notes |
|------|---------|-------|
| Docker | 24+ | **Must be running** (not just installed) |
| Docker Compose | v2 | Usually bundled with Docker Desktop |
| Git | 2.x | For cloning repos |

> ⚠️ **Docker must be running.** The CLI checks this during setup and will warn you if Docker is installed but the daemon isn't started.
>
> No Node.js or Python needed — all services run as pre-built Docker containers.

For **remote setup** (connecting to an existing server): just the CLI binary. No other dependencies.

---

## Cookbook

Real workflows, copy-paste ready. Each recipe is a complete flow.

### 🚀 First Time Setup (Local)

```bash
reposwarm new --local             # Bootstrap everything: Temporal, API, Worker, UI
reposwarm status                  # Verify connection
reposwarm doctor                  # Full health check
reposwarm show ui                 # Open the web UI
```

### 🚀 First Time Setup (Remote Server)

```bash
reposwarm config init             # Interactive setup (enter API URL + token)
reposwarm status                  # Verify connection
```

---

### 🔍 Run Your First Investigation

```bash
reposwarm repos add my-app --url https://github.com/org/my-app
reposwarm investigate my-app      # Pre-flight checks run automatically
reposwarm wf progress             # Watch it work
reposwarm results sections my-app # Browse the results
reposwarm results read my-app     # Read the full report
```

### 🔍 Investigate All Repos

```bash
reposwarm investigate --all --parallel 3
reposwarm dashboard               # Live TUI — progress for every repo
```

### 🔍 Check Before You Start (Dry Run)

```bash
reposwarm preflight my-app        # System + repo readiness check
reposwarm investigate my-app --dry-run  # Pre-flight only, no workflow created
```

---

### 🩺 Something's Stuck — Full Diagnosis

The 2-minute flow that finds any issue:

```bash
# Step 1: What's wrong?
reposwarm doctor

# Step 2: Full install log (paged for humans, text dump for agents)
reposwarm debug-logs

# Step 3: Check for stalled workflows + worker errors
reposwarm errors

# Step 3: Zoom in on a specific workflow
reposwarm wf status <workflow-id> -v

# Step 4: See the Temporal event timeline
reposwarm wf history <workflow-id>

# Step 5: Check worker logs directly
reposwarm logs worker -f
```

**What `doctor` catches:**
- Missing env vars — driven by `providers.json` (single source of truth for LLM + git provider requirements)
- Git provider auth: GITHUB_TOKEN, GITLAB_TOKEN, AZURE_DEVOPS_PAT, etc. (based on configured git provider)
- Worker log runtime errors (crashes, tracebacks — env validation noise filtered out)
- Per-worker health in multi-worker setups
- Stalled workflows with zero progress
- Config, API, Temporal, DynamoDB connectivity
- Version compatibility (API ↔ CLI)
- CLI update availability (checks GitHub releases)

**What `errors` catches:**
- Activity failures, timeouts, workflow failures
- Stalled activities (scheduled but never started — worker can't pick them up)
- Zero-progress workflows (running > 30 min with nothing completed)
- Worker startup/validation failures

---

### 🔧 Fix Missing Environment Variables

When `doctor` tells you env vars are missing:

```bash
# See what's set and what's missing
reposwarm config worker-env list

# Set the missing vars
reposwarm config worker-env set ANTHROPIC_API_KEY sk-ant-your-key-here
reposwarm config worker-env set GITHUB_TOKEN ghp_your-token-here

# Restart the worker to pick up changes
reposwarm restart worker --wait

# Verify it's healthy now
reposwarm doctor
```

### 🔧 Configure LLM Provider

RepoSwarm supports three provider backends:

| Provider | Auth | Best for |
|----------|------|----------|
| `anthropic` | API key | Direct Anthropic API access |
| `bedrock` | AWS credentials (IAM role, access keys, SSO, profile, or Bedrock API keys) | AWS-native environments |
| `litellm` | Proxy URL + optional key | **OpenAI, Gemini, Mistral, and 100+ other models** via [LiteLLM](https://github.com/BerriAI/litellm) proxy |

> **OpenAI and Gemini** are not built-in providers — use `litellm` to route to them. See the [LiteLLM section](#using-openai-gemini-or-other-models-via-litellm) below.

#### Anthropic (Direct API)

```bash
# Interactive
reposwarm config provider setup

# Non-interactive
reposwarm config provider setup \
  --provider anthropic \
  --api-key sk-ant-api03-YOUR-KEY \
  --model sonnet \
  --non-interactive
```

#### Amazon Bedrock

```bash
# IAM role (EC2 instance profile / ECS task role — recommended)
reposwarm config provider setup \
  --provider bedrock \
  --auth-method iam-role \
  --region us-east-1 \
  --model opus \
  --non-interactive

# AWS access keys
reposwarm config provider setup \
  --provider bedrock \
  --auth-method access-keys \
  --aws-key AKIA... \
  --aws-secret wJalr... \
  --region us-east-1 \
  --model sonnet \
  --non-interactive

# AWS SSO / Identity Center
reposwarm config provider setup \
  --provider bedrock \
  --auth-method sso \
  --aws-profile my-sso-profile \
  --region us-west-2 \
  --model sonnet \
  --non-interactive

# Named AWS profile (~/.aws/credentials)
reposwarm config provider setup \
  --provider bedrock \
  --auth-method profile \
  --aws-profile prod \
  --region us-east-1 \
  --model opus \
  --non-interactive

# Bedrock API keys (from AWS Console → Bedrock → API keys)
reposwarm config provider setup \
  --provider bedrock \
  --auth-method api-keys \
  --bedrock-key br-... \
  --region us-east-1 \
  --model sonnet \
  --non-interactive
```

> **How IAM role auth works in Docker:** The worker container runs with `network_mode: host`, sharing the host's network stack. This lets the AWS SDK inside the container transparently access EC2 instance metadata (IMDSv2) or any credentials configured on the host (`~/.aws/`), exactly like it would in ECS Fargate. No credential injection or special configuration needed.

#### Using OpenAI, Gemini, or Other Models via LiteLLM

RepoSwarm uses Claude under the hood, so non-Anthropic models require a [LiteLLM](https://github.com/BerriAI/litellm) proxy to translate between APIs. LiteLLM supports 100+ models including OpenAI, Gemini, Mistral, Cohere, and more.

**1. Start a LiteLLM proxy** (one-time setup):

```bash
pip install litellm

# OpenAI
litellm --model openai/gpt-4o --port 4000

# Google Gemini
GEMINI_API_KEY=your-key litellm --model gemini/gemini-2.5-pro --port 4000

# Or use a config file for multiple models:
# See https://docs.litellm.ai/docs/proxy/configs
litellm --config litellm_config.yaml --port 4000
```

**2. Point RepoSwarm at the proxy:**

```bash
# OpenAI via LiteLLM
reposwarm config provider setup \
  --provider litellm \
  --proxy-url http://localhost:4000 \
  --proxy-key sk-your-litellm-key \
  --model openai/gpt-4o \
  --non-interactive

# Google Gemini via LiteLLM
reposwarm config provider setup \
  --provider litellm \
  --proxy-url http://localhost:4000 \
  --model gemini/gemini-2.5-pro \
  --non-interactive

# Any other LiteLLM-supported model
reposwarm config provider setup \
  --provider litellm \
  --proxy-url https://my-proxy.example.com \
  --proxy-key sk-proxy-key \
  --model mistral/mistral-large-latest \
  --non-interactive
```

> **Note:** Investigation quality may vary with non-Claude models. RepoSwarm's prompts and workflows are optimized for Claude Sonnet/Opus.

#### Provider Management

```bash
# Quick-switch provider (keeps other settings)
reposwarm config provider set bedrock
reposwarm config provider set anthropic --check

# Check what's configured (detects CLI vs worker drift)
reposwarm config provider show

# List available model aliases
reposwarm config model list

# Pin model versions (prevents silent version drift)
reposwarm config model pin

# Restart worker to apply changes
reposwarm restart worker
```

### 🔧 Configure Git Provider

Tell RepoSwarm which git hosting platform your repos are on, so it knows what credentials to require:

```bash
# Interactive setup
reposwarm config git setup

# Direct set
reposwarm config git set github        # GitHub (needs GITHUB_TOKEN)
reposwarm config git set codecommit    # AWS CodeCommit (needs IAM + AWS CLI)
reposwarm config git set gitlab        # GitLab (needs GITLAB_TOKEN)
reposwarm config git set azure         # Azure DevOps (needs AZURE_DEVOPS_PAT + ORG)
reposwarm config git set bitbucket     # Bitbucket (needs USERNAME + APP_PASSWORD)

# Show current config + required env vars
reposwarm config git show
```

After setting the git provider, `doctor` will check for the correct authentication variables:

```bash
reposwarm doctor
# Worker Environment
#   ✗ GITHUB_TOKEN — NOT SET — GitHub personal access token
#      Set it: reposwarm config worker-env set GITHUB_TOKEN <value>
```

### 📋 View Changelog

```bash
reposwarm changelog                    # Current version's release notes
reposwarm changelog v1.3.50            # Specific version
reposwarm changelog latest             # Latest release
reposwarm changelog --all              # All releases with dates
reposwarm changelog --since v1.3.45    # Cumulative changes since a version
```

### ⬆️ Upgrade Components

```bash
reposwarm upgrade                      # Upgrade CLI to latest
reposwarm upgrade api                  # Upgrade API server (git pull + build + restart)
reposwarm upgrade ui                   # Upgrade UI
reposwarm upgrade all                  # Upgrade all components
```

### 🌐 Remote Access via SSH Tunnel

When RepoSwarm runs on a remote server (EC2, VPS), access the web UI through an SSH tunnel:

```bash
reposwarm tunnel                       # Shows copy-paste SSH tunnel command
```

### 🗑️ Uninstall

```bash
reposwarm uninstall                    # Interactive removal
reposwarm uninstall --force            # Skip confirmation
reposwarm uninstall --keep-config      # Keep ~/.reposwarm for reinstall
```

For local installs: stops all services, runs `docker compose down`, removes files.
For remote CLI-only setups: removes the binary and config, warns about the server.

### 🔧 Fix a Stuck Investigation (End-to-End)

The full fix loop — from broken to running in 6 commands:

```bash
reposwarm doctor                  # → finds: ANTHROPIC_API_KEY NOT SET
reposwarm config worker-env set ANTHROPIC_API_KEY sk-ant-xxx
reposwarm config worker-env set GITHUB_TOKEN ghp_xxx
reposwarm restart worker --wait   # → ✅ worker-1 healthy
reposwarm wf retry <workflow-id>  # → terminates old, starts new
reposwarm wf progress             # → watch it go
```

### 🔧 Replace a Stuck Investigation

```bash
# Terminates any existing workflow for this repo, then starts fresh
reposwarm investigate my-app --replace
```

### 🔧 Retry a Failed Investigation with a Different Model

```bash
reposwarm wf retry <workflow-id> --model us.anthropic.claude-opus-4-6
```

---

### 📊 Monitor Running Investigations

```bash
# Live dashboard (htop-style, all repos)
reposwarm dashboard

# Focus on one repo
reposwarm dashboard --repo my-app

# Watch a specific workflow with step checklist
reposwarm wf progress --repo my-app --wait

# Quick snapshot for scripts/agents
reposwarm dashboard --json
reposwarm errors --json
```

### 📊 Check Worker Health

```bash
# List all workers
reposwarm workers list

# Deep-dive on a specific worker
reposwarm workers show worker-1

# See all services (Docker containers or processes)
reposwarm services
```

---

### 🔎 Search and Explore Results

```bash
# Search across all investigations
reposwarm results search "authentication" --max 10

# Read a specific section
reposwarm results read my-app authentication

# Compare two repos
reposwarm results diff repo-a repo-b

# Export everything to markdown files
reposwarm results export --all -d ./docs

# Audit completeness (all 17 sections present?)
reposwarm results audit
```

---

### 📖 Query Architecture Results — `ask` CLI

Architecture querying has moved to the standalone [`ask`](https://github.com/reposwarm/ask-cli) CLI.

**RepoSwarm writes** architecture docs (investigations). **`ask` reads** them.

```bash
# Install ask
curl -fsSL https://raw.githubusercontent.com/reposwarm/ask-cli/main/install.sh | sh

# Set up local askbox (auto-detects RepoSwarm config)
ask setup

# Query your architecture
ask "how does auth work across services?"
ask results list
ask results search "DynamoDB"
ask results export --all -d ./docs
```

See the [ask README](https://github.com/reposwarm/ask-cli) for full documentation.

> **Note:** `reposwarm ask --arch` and `reposwarm results` still work but will show a deprecation notice pointing to the `ask` CLI.

---

### 🧹 Cleanup and Maintenance

```bash
# Clean up old workflows (completed/failed/terminated)
reposwarm wf prune --older 7d --dry-run    # Preview first
reposwarm wf prune --older 7d -y           # Actually prune

# Prune only failed workflows
reposwarm wf prune --status failed -y

# Gracefully cancel a running investigation (finishes current step)
reposwarm wf cancel <workflow-id>

# Hard terminate (immediate)
reposwarm wf terminate <workflow-id> -y
```

---

### ⚙️ Service Management (Local Mode)

```bash
# See what's running
reposwarm services

# Restart everything
reposwarm restart

# Restart just the worker
reposwarm restart worker --wait

# Stop/start individual services
reposwarm stop worker
reposwarm start worker --wait

# Check all service URLs
reposwarm url all
```

---

### 🤖 Agent / Script Integration

Every command supports `--json` for structured output and `--for-agent` for plain text:

```bash
# Check system health programmatically
reposwarm doctor --json | jq '.checks[] | select(.status=="fail")'

# Get all errors as JSON
reposwarm errors --json | jq '.workerErrors, .stalls, .errors'

# Single dashboard snapshot (no TUI, exits immediately)
reposwarm dashboard --json

# Pre-flight check before scripted investigation
reposwarm preflight my-app --json | jq '.ready'

# Investigate with no prompts
reposwarm investigate my-app --force --json

# Worker status for monitoring
reposwarm workers list --json | jq '.workers[] | {name, status, envStatus}'

# Service health for monitoring
reposwarm services --json | jq '.[] | {name, status, pid}'
```

---

## Command Reference

### Setup & Configuration

| Command | Description |
|---------|-------------|
| `reposwarm config init` | Interactive setup wizard |
| `reposwarm config show` | Display config (includes server config + model drift warning) |
| `reposwarm config set <key> <value>` | Update a config value |
| `reposwarm config server` | View server-side config |
| `reposwarm config server-set <key> <value>` | Update server config |
| `reposwarm config worker-env list` | Show worker env vars (`--reveal` for unmasked values) |
| `reposwarm config worker-env set <K> <V>` | Set worker env var (`--restart` to auto-restart) |
| `reposwarm config worker-env unset <K>` | Remove worker env var |
| `reposwarm config provider setup` | Interactive provider setup (Anthropic, Bedrock, LiteLLM) |
| `reposwarm config provider set <provider>` | Quick-switch provider (`anthropic`, `bedrock`, `litellm`) |
| `reposwarm config provider show` | Show provider config + drift detection |
| `reposwarm config model set <alias\|id>` | Set model (resolves aliases per provider, `--sync` to worker) |
| `reposwarm config model show` | Show model across CLI, server, and worker |
| `reposwarm config model list` | List aliases with resolved IDs per provider |
| `reposwarm config model pin` | Pin all model aliases to current versions |
| `reposwarm new --local` | Bootstrap complete local installation |
| `reposwarm upgrade` | Self-update (`--force` to reinstall) |

### Diagnostics

| Command | Description |
|---------|-------------|
| `reposwarm status` | Quick API health + latency |
| `reposwarm doctor` | Full diagnosis: config, API, Temporal, workers, env, logs, stalls |
| `reposwarm preflight [repo]` | Verify system readiness for an investigation |
| `reposwarm errors` | Errors + stalls + worker failures (`--repo`, `--stall-threshold`) |
| `reposwarm logs [service]` | View service logs (`-f` follow, `-n` lines) |

### Workers & Services

| Command | Description |
|---------|-------------|
| `reposwarm workers list` | All workers: health, queue, env status (`-v` for PID/host) |
| `reposwarm workers show <name>` | Worker deep-dive: env, logs, tasks |
| `reposwarm services` | Process table: PID, status, port, manager |
| `reposwarm restart [service]` | Restart one or all (`--wait`, `--timeout`) |
| `reposwarm stop <service>` | Graceful stop |
| `reposwarm start <service>` | Start service (`--wait` for health check) |

### Repositories

| Command | Description |
|---------|-------------|
| `reposwarm repos list` | List repos (`--source`, `--filter`, `--enabled`) |
| `reposwarm repos show <name>` | Detailed repo view |
| `reposwarm repos add <name>` | Add repo (`--url`, `--source`) |
| `reposwarm repos remove <name>` | Remove (`-y` skip confirm) |
| `reposwarm repos enable/disable <name>` | Toggle investigation eligibility |
| `reposwarm repos discover` | Auto-discover repos from your configured git provider (GitHub, GitLab, CodeCommit, Azure DevOps, Bitbucket) |

### Investigation & Workflows

| Command | Description |
|---------|-------------|
| `reposwarm investigate <repo>` | Start investigation (pre-flight auto-runs) |
| | `--force` skip pre-flight, `--replace` terminate existing, `--dry-run` |
| `reposwarm investigate --all` | All enabled repos (`--parallel`) |
| `reposwarm wf list` | List recent workflows (`--limit`) |
| `reposwarm wf status <id>` | Workflow details (`-v` for activities + worker attribution) |
| `reposwarm wf history <id>` | Temporal event timeline (`--filter`, `--limit`) |
| `reposwarm wf progress` | Progress across repos (`--repo`, `--wait`) |
| `reposwarm wf watch [id]` | Live watch (`--interval`) |
| `reposwarm wf retry <id>` | Terminate + re-investigate (`-y`, `--model`) |
| `reposwarm wf cancel <id>` | Graceful cancel (current activity completes) |
| `reposwarm wf terminate <id>` | Hard stop (`-y`, `--reason`) |
| `reposwarm wf prune` | Cleanup old workflows (`--older`, `--status`, `--dry-run`) |
| `reposwarm dashboard` | Live TUI (`--repo` focused, `--json` single snapshot) |

### Results & Analysis

| Command | Description |
|---------|-------------|
| `reposwarm results list` | Repos with results |
| `reposwarm results sections <repo>` | Section list |
| `reposwarm results read <repo> [section]` | Read results (`--raw` for markdown) |
| `reposwarm results search <query>` | Full-text search (`--repo`, `--section`, `--max`) |
| `reposwarm results export <repo> -o file.md` | Export to file |
| `reposwarm results export --all -d ./docs` | Export all |
| `reposwarm results audit` | Validate completeness |
| `reposwarm results diff <repo1> <repo2>` | Compare investigations |
| `reposwarm results report [repos...] -o f.md` | Consolidated report |

### Architecture Queries (Askbox)

| Command | Description |
|---------|-------------|
| `reposwarm ask <question>` | Quick Q&A about RepoSwarm usage |
| `reposwarm ask --arch <question>` | Architecture analysis via askbox agent |
| | `--repos` scope to repos, `--adapter` choose agent, `--no-wait` async |

### Prompts

| Command | Description |
|---------|-------------|
| `reposwarm prompts list` | List prompts |
| `reposwarm prompts show <name>` | View template (`--raw`) |
| `reposwarm prompts create/update/delete <name>` | Manage prompts |
| `reposwarm prompts toggle <name>` | Enable/disable |

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | JSON output |
| `--for-agent` | Plain text (no colors/formatting) |
| `--api-url <url>` | Override API URL |
| `--api-token <token>` | Override API token |
| `--verbose` | Debug info |
| `-v` / `--version` | Print version |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `REPOSWARM_API_URL` | Override API URL |
| `REPOSWARM_API_TOKEN` | Override bearer token |

### Provider Environment Variables

These are set automatically by `config provider setup` — you normally don't need to touch them:

| Variable | When | Description |
|----------|------|-------------|
| `CLAUDE_CODE_USE_BEDROCK` | Bedrock | Set to `1` to enable Bedrock mode |
| `AWS_REGION` | Bedrock | Required (Bedrock doesn't read `.aws/config`) |
| `ANTHROPIC_MODEL` | All | Primary model ID |
| `ANTHROPIC_SMALL_FAST_MODEL` | All | Fast/cheap model for triage |
| `ANTHROPIC_BASE_URL` | LiteLLM | Proxy endpoint URL |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | Bedrock | Pinned Opus version |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | Bedrock | Pinned Sonnet version |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | Bedrock | Pinned Haiku version |

### Git Provider Environment Variables

Set via `config git set <provider>` — determines which credentials are required:

| Variable | Git Provider | Description |
|----------|-------------|-------------|
| `GITHUB_TOKEN` | GitHub | Personal access token (scopes: repo) |
| `GITLAB_TOKEN` | GitLab | Personal/project access token |
| `GITLAB_URL` | GitLab | Instance URL (optional, default: gitlab.com) |
| `AZURE_DEVOPS_PAT` | Azure DevOps | Personal access token |
| `AZURE_DEVOPS_ORG` | Azure DevOps | Organization URL |
| `BITBUCKET_USERNAME` | Bitbucket | Bitbucket username |
| `BITBUCKET_APP_PASSWORD` | Bitbucket | App password |
| `AWS_REGION` | CodeCommit | AWS region (uses IAM for auth) |

### Model Alias Resolution

| Alias | Anthropic API | Amazon Bedrock |
|-------|---------------|----------------|
| `sonnet` | `claude-sonnet-4-6` | `us.anthropic.claude-sonnet-4-6` |
| `opus` | `claude-opus-4-6` | `us.anthropic.claude-opus-4-6-v1` |
| `haiku` | `claude-haiku-4-5` | `us.anthropic.claude-haiku-4-5-20251001-v1:0` |

## Configurable Keys

Set via `reposwarm config set <key> <value>`:

| Key | Default | Description |
|-----|---------|-------------|
| `apiUrl` | `http://localhost:3000/v1` | API server URL |
| `apiToken` | — | Bearer token |
| `region` | `us-east-1` | AWS region |
| `defaultModel` | `us.anthropic.claude-sonnet-4-6` | Default LLM model |
| `chunkSize` | `10` | Files per investigation chunk |
| `installDir` | `~/.reposwarm` | Local installation directory |
| `temporalPort` | `7233` | Temporal gRPC port |
| `temporalUiPort` | `8233` | Temporal UI port |
| `apiPort` | `3000` | API server port |
| `uiPort` | `3001` | Web UI port |
| `hubUrl` | — | Project hub URL |

| `provider` | LLM provider (`anthropic`, `bedrock`, `litellm`) |
| `awsRegion` | AWS region for Bedrock |
| `proxyUrl` | LiteLLM proxy URL |
| `proxyKey` | LiteLLM proxy API key |
| `smallModel` | Fast/cheap model for triage tasks |

## Development

```bash
go test ./...              # Tests
go vet ./...               # Lint
go build ./cmd/reposwarm   # Build
```

CI: GitHub push → Go 1.24 tests → Cross-compile (darwin/linux × arm64/amd64) → GitHub Release

## Ecosystem

| Project | Docker Image |
|---------|-------------|
| [reposwarm](https://github.com/reposwarm/reposwarm) (worker) | `ghcr.io/reposwarm/worker:latest` |
| [reposwarm-api](https://github.com/reposwarm/reposwarm-api) | `ghcr.io/reposwarm/api:latest` |
| [reposwarm-ui](https://github.com/reposwarm/reposwarm-ui) | `ghcr.io/reposwarm/ui:latest` |
| **reposwarm-cli** (this repo) | — (binary install) |
| [reposwarm-askbox](https://github.com/reposwarm/reposwarm-askbox) | `ghcr.io/reposwarm/askbox:latest` |

All Docker images are multi-arch (`linux/amd64` + `linux/arm64`), published automatically on every push to `main`.

## License

MIT
