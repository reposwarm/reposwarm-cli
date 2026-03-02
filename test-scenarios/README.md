# RepoSwarm CLI Test Scenarios

End-to-end test scenarios that exercise the full CLI against a live RepoSwarm stack.

## Structure

Each `.sh` file is a self-contained test scenario. They share `_common.sh` for setup/assertions.

## Running

```bash
# Run all scenarios (on a fresh EC2 instance with the stack running)
./run-all.sh

# Run a single scenario
./01-bootstrap-local.sh
```

## Scenarios

| # | File | What it tests |
|---|------|---------------|
| 01 | bootstrap-local | `reposwarm new --local` full stack bootstrap |
| 02 | config-lifecycle | `config init`, `config set`, `config show`, `config server` |
| 03 | health-status | `status`, `doctor`, `preflight`, `services` |
| 04 | repo-management | `repos add/list/show/disable/enable/remove` |
| 05 | investigate-single | Single repo investigation + workflow tracking |
| 06 | worker-env | `config worker-env list/set/unset` via API |
| 07 | logs-services | `logs`, `services`, `restart/stop/start` |
| 08 | results-browsing | `results list/read/search/export/diff/report` |
| 09 | workflows-ops | `wf list/status/history/progress/cancel/prune` |
| 10 | json-agent-mode | All commands with `--json` and `--for-agent` flags |
| 11 | error-handling | Bad inputs, missing config, unreachable API |
| 12 | workers-inspect | `workers list/show` via API |

## Requirements

- Ubuntu 24.04 ARM64 (t4g.medium recommended)
- Internet access (GitHub, Docker Hub)
- ~10GB disk
- No pre-existing RepoSwarm installation
