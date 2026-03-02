# reposwarm-cli

CLI for [RepoSwarm](https://github.com/loki-bedlam/reposwarm-ui) — AI-powered multi-repo architecture discovery.

Written in Go. Single 9MB binary, zero runtime dependencies, 4ms startup.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/loki-bedlam/reposwarm-cli/main/install.sh | sh
```

Or build from source:
```bash
git clone https://github.com/loki-bedlam/reposwarm-cli.git
cd reposwarm-cli
go build -o reposwarm ./cmd/reposwarm
```

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

# Step 2: Check for stalled workflows + worker errors
reposwarm errors

# Step 3: Zoom in on a specific workflow
reposwarm wf status <workflow-id> -v

# Step 4: See the Temporal event timeline
reposwarm wf history <workflow-id>

# Step 5: Check worker logs directly
reposwarm logs worker -f
```

**What `doctor` catches:**
- Missing env vars (ANTHROPIC_API_KEY, GITHUB_TOKEN, AWS credentials)
- Worker log errors and tracebacks
- Per-worker health in multi-worker setups
- Stalled workflows with zero progress
- Config, API, Temporal, DynamoDB connectivity

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

# See all services (PID, port, status)
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
| `reposwarm repos discover` | Auto-discover CodeCommit repos |

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

## Configurable Keys

Set via `reposwarm config set <key> <value>`:

| Key | Default | Description |
|-----|---------|-------------|
| `apiUrl` | `http://localhost:3000/v1` | API server URL |
| `apiToken` | — | Bearer token |
| `region` | `us-east-1` | AWS region |
| `defaultModel` | `us.anthropic.claude-sonnet-4-6` | Default LLM model |
| `chunkSize` | `10` | Files per investigation chunk |
| `installDir` | `~/reposwarm` | Local installation directory |
| `temporalPort` | `7233` | Temporal gRPC port |
| `temporalUiPort` | `8233` | Temporal UI port |
| `apiPort` | `3000` | API server port |
| `uiPort` | `3001` | Web UI port |
| `hubUrl` | — | Project hub URL |

## Development

```bash
go test ./...              # Tests
go vet ./...               # Lint
go build ./cmd/reposwarm   # Build
```

CI: GitHub push → Go 1.24 tests → Cross-compile (darwin/linux × arm64/amd64) → GitHub Release

## License

MIT
