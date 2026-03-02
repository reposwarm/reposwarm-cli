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

## Quick Start

### Connect to Existing Server
```bash
reposwarm config init             # Connect to a RepoSwarm server
reposwarm status                  # Check connection
reposwarm repos list              # List tracked repos
reposwarm investigate <repo>      # Run an investigation
reposwarm results sections <repo> # Browse results
```

### Bootstrap Local Installation
```bash
reposwarm new --local             # Set up complete local environment
# This will:
#   1. Clone worker, API, and UI repositories
#   2. Start Temporal server + DynamoDB local
#   3. Start worker processes
#   4. Launch API server and UI
#   5. Configure CLI to connect to local API

reposwarm show temporal           # Open Temporal UI
reposwarm show ui                 # Open RepoSwarm UI
reposwarm url all                 # View all service URLs
```

## Commands

### Setup & Diagnostics
| Command | Description |
|---------|-------------|
| `reposwarm config init` | Interactive setup wizard |
| `reposwarm config show` | Display current config |
| `reposwarm config set <key> <value>` | Update config value |
| `reposwarm config server` | View server-side config |
| `reposwarm config server-set <key> <value>` | Update server config |
| `reposwarm status` | Quick API health check with latency |
| `reposwarm doctor` | Deep diagnosis (config, API, Temporal, DynamoDB, worker, network) |
| `reposwarm new` | Bootstrap a new local installation (`--local` for complete setup) |
| `reposwarm show <target>` | Open URL in browser (temporal, ui, api, hub) |
| `reposwarm url <service>` | Print service URL (temporal, temporal-grpc, ui, api, hub, all) |
| `reposwarm version` | Print version (`-v` / `--version` also work) |
| `reposwarm upgrade` | Self-update to latest version (`--force` to reinstall) |

### Repositories
| Command | Description |
|---------|-------------|
| `reposwarm repos list` | List all tracked repos (`--source`, `--filter`, `--enabled`) |
| `reposwarm repos show <name>` | Detailed single repo view |
| `reposwarm repos add <name>` | Add a repo (`--url`, `--source`) |
| `reposwarm repos remove <name>` | Remove a repo (`-y` skip confirm) |
| `reposwarm repos enable <name>` | Enable for investigation |
| `reposwarm repos disable <name>` | Disable from investigation |
| `reposwarm repos discover` | Auto-discover CodeCommit repos |

### Investigation & Workflows
| Command | Description |
|---------|-------------|
| `reposwarm investigate <repo>` | Investigate single repo (`--model`, `--chunk-size`) |
| `reposwarm investigate --all` | Investigate all enabled repos (`--parallel`) |
| `reposwarm workflows list` | List recent workflows (`--limit`) |
| `reposwarm workflows status <id>` | Workflow details (`-v` for activity checklist) |
| `reposwarm workflows history <id>` | Temporal event history (`--filter`, `--run-id`, `--limit`) |
| `reposwarm workflows progress` | Show investigation progress across repos |
| `reposwarm workflows watch [id]` | Watch workflows in real-time (`--interval`) |
| `reposwarm workflows terminate <id>` | Stop a workflow (`-y`, `--reason`) |
| `reposwarm workflows retry <id>` | Terminate + re-investigate same repo (`-y`, `--model`) |
| `reposwarm workflows cancel <id>` | Graceful cancellation — current activity completes first |
| `reposwarm workflows prune` | Clean up old workflows (`--older`, `--status`, `--dry-run`) |

### Monitoring & Debugging
| Command | Description |
|---------|-------------|
| `reposwarm dashboard` | Live TUI dashboard (`--repo` for focused view) |
| `reposwarm errors` | List errors + stall warnings + worker failures (`--repo`, `--stall-threshold`) |

### Workers & Services

| Command | Description |
|---------|-------------|
| `reposwarm workers list` | List all workers with health, task queue, env status (`--verbose`) |
| `reposwarm workers show <name>` | Deep-dive on a worker: env, logs, tasks (`--logs`, `--no-logs`) |
| `reposwarm services` | Process table — all services with PID, status, port, manager |
| `reposwarm restart [service]` | Restart one or all services (`--wait`, `--timeout`) |
| `reposwarm stop <service>` | Stop a service (graceful SIGTERM) |
| `reposwarm start <service>` | Start a service (`--wait` for health check) |
| `reposwarm preflight [repo]` | Verify system readiness without starting an investigation |
| `reposwarm logs [service]` | View local service logs (`-f` to follow, `-n` lines) |

### Results & Analysis
| Command | Description |
|---------|-------------|
| `reposwarm results list` | Repos with investigation results |
| `reposwarm results sections <repo>` | List sections for a repo |
| `reposwarm results read <repo> [section]` | Read results (`--raw` for markdown) |
| `reposwarm results meta <repo> [section]` | Metadata only |
| `reposwarm results export <repo> -o file.md` | Export to file |
| `reposwarm results export --all -d ./docs` | Export all repos to directory |
| `reposwarm results search <query>` | Search results (`--repo`, `--section`, `--max`) |
| `reposwarm results audit` | Validate all repos have complete sections |
| `reposwarm results diff <repo1> <repo2>` | Compare investigations |
| `reposwarm results report [repos...] -o f.md` | Consolidated report (`--sections`) |

### Prompts
| Command | Description |
|---------|-------------|
| `reposwarm prompts list` | List prompts (derives from results if API returns empty) |
| `reposwarm prompts show <name>` | Show template (`--raw`) |
| `reposwarm prompts create <name>` | Create (`--type`, `--template-file`) |
| `reposwarm prompts update <name>` | Update template/description |
| `reposwarm prompts delete <name>` | Delete |
| `reposwarm prompts toggle <name>` | Toggle enabled/disabled |

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | JSON output (agent/script-friendly) |
| `--for-agent` | Plain text output for agents/scripts |
| `--api-url <url>` | Override API URL |
| `--api-token <token>` | Override API token |
| `--verbose` | Debug info |
| `-v` / `--version` | Print version |

Default output is human-friendly (colors, tables). Use `--for-agent` for plain text or `--json` for structured output.

## Local Development Setup

The `reposwarm new --local` command provides a complete local development environment:

### What it does:
1. **Clones repositories** — worker, API, and UI from GitHub
2. **Starts Temporal** — Local Temporal server on port 7233 (UI on 8233)
3. **Starts DynamoDB Local** — Local AWS DynamoDB on port 8000
4. **Configures Worker** — Sets up environment, installs dependencies
5. **Starts API** — Launches API server on port 3000
6. **Starts UI** — Launches web UI on port 3001
7. **Configures CLI** — Connects CLI to local API

### Requirements:
- Docker (for Temporal and DynamoDB)
- Node.js 18+ (for UI)
- Python 3.11+ (for worker and API)
- ~2GB disk space
- ~1GB RAM

### Usage:
```bash
# Bootstrap everything
reposwarm new --local

# Check status
reposwarm status
reposwarm doctor

# Open services in browser
reposwarm show temporal   # Temporal UI
reposwarm show ui         # RepoSwarm UI
reposwarm show api        # API health endpoint

# View all service URLs
reposwarm url all

# Start investigating
reposwarm repos add my-repo --url https://github.com/user/repo
reposwarm investigate my-repo
```

### Configuration:
Customize ports and URLs via `reposwarm config set`:
```bash
reposwarm config set temporalPort 7233
reposwarm config set temporalUiPort 8233
reposwarm config set apiPort 3000
reposwarm config set uiPort 3001
reposwarm config set hubUrl https://github.com/your-org/reposwarm-ui
```

## Debugging Stuck Investigations

When an investigation seems stuck, use these commands to diagnose:

```bash
# 1. Full system health — now checks worker env vars and log errors
reposwarm doctor

# 2. Errors + stall detection — flags activities that never started
reposwarm errors

# 3. Activity-level status — which step is running/stuck?
reposwarm wf status <workflow-id> -v

# 4. View full Temporal event history
reposwarm wf history <workflow-id> --filter Activity

# 5. Tail worker logs (local mode)
reposwarm logs worker -f

# 6. Fix the issue, then retry the stuck workflow
reposwarm wf retry <workflow-id>
```

### What `doctor` checks (local mode):
- Config, API, Temporal, DynamoDB, Worker connectivity
- **Worker environment**: ANTHROPIC_API_KEY, GITHUB_TOKEN, AWS credentials
- **Worker logs**: scans last 20 lines for errors/tracebacks
- Local tools (Git, Docker, Node, Python, AWS CLI), network

### What `errors` detects:
- Activity failures, timeouts, workflow failures
- **Stalled activities**: scheduled but never started (worker can't pick up tasks)
- **Zero-progress workflows**: running > 30 min with no completed steps

## Environment Variables

| Variable | Description |
|----------|-------------|
| `REPOSWARM_API_URL` | API server URL |
| `REPOSWARM_API_TOKEN` | Bearer token |

## Agent Usage

Every command supports `--json`:

```bash
reposwarm repos list --json | jq '.[].name'
reposwarm results read my-repo --json | jq '.content'
reposwarm doctor --json | jq '.checks[] | select(.status=="fail")'
```

## Development

```bash
go test ./...              # Tests
go vet ./...               # Lint
go build ./cmd/reposwarm   # Build
```

## CI/CD

CodePipeline (`reposwarm-cli-pipeline`):
GitHub push → CodeBuild (Go 1.24, ARM64) → Tests → Cross-compile 4 targets → GitHub Release

## License

MIT
