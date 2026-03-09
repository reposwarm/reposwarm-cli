# Changelog

## Unreleased

### Bug Fixes

- **doctor: fix Anthropic API key reported as NOT SET for Docker installs** — `checkProviderCredentials()` was fetching environment variables exclusively from the API endpoint (`/workers/worker-1/env?reveal=true`), which returns empty results for Docker-based installations. The fix applies the same Docker-aware pattern already used by `checkWorkerEnv()`: detecting the install type and reading from the local `worker.env` file via `bootstrap.ReadWorkerEnvFile()` for Docker installs, falling back to the API endpoint for source installs.

---

## 2026-03-08 — Bulk terminate and workflow prune fix

### Changes

- `internal/commands/workflows.go` — Added `--all` flag to `workflows terminate`. Running `reposwarm workflows terminate --all --yes` now terminates all running workflows without interactive prompts. Previously each workflow had to be terminated individually.
- `internal/commands/workflows_prune.go` — Fixed `workflows prune` to actually delete workflows from Temporal history using `DELETE /workflows/<id>` instead of just re-terminating them. Previously prune reported success but workflows remained in the list.

### Usage

```
reposwarm workflows terminate --all --yes    # Terminate all running workflows
reposwarm workflows prune --yes              # Delete old completed/failed/terminated workflows
```

---

## 2026-03-08 — Sequential investigation mode (`--parallel` flag)

### Problem

Running `reposwarm investigate --all` on resource-constrained machines (16GB RAM) caused Temporal deadlock errors (`TMPRL1101`). The CLI fired `POST /investigate/single` for all repos simultaneously, starting 7+ concurrent Temporal workflows. The worker had no concurrency limits, so all repos cloned and analyzed in parallel, saturating I/O/CPU and blocking the Temporal event loop for >2 seconds.

### Solution

Single control point: the `--parallel` flag on `investigate --all` now controls both CLI dispatch behaviour and worker concurrency. No separate env vars to remember.

```
reposwarm investigate --all                  # Unchanged (fire-and-forget, parallel)
reposwarm investigate --all --parallel=1     # Sequential: one repo at a time
reposwarm investigate --all --parallel=2     # Batched: two repos at a time
```

When `--parallel` is set, the CLI:
1. Writes `REPOSWARM_PARALLEL=N` to `worker.env` (skips if already set)
2. Restarts the worker to apply the new concurrency limit (only if value changed)
3. Checks for running workflows before restarting to avoid killing in-flight work
4. Dispatches repos sequentially (or in batches of N), polling for completion between each

### Changes

**lac-reposwarm-cli (Go CLI):**

- `internal/commands/investigate.go` — Rewrote `--all` loop with three modes: sequential (`--parallel=0` or `1`), batched (`--parallel=N`), and fire-and-forget (no flag, unchanged). Updated help text with examples.
- `internal/commands/investigate_helpers.go` — Added `waitForWorkflow()` (polls `GET /workflows/<id>` until terminal state) and `ensureWorkerParallel()` (writes env var, restarts worker if changed, aborts if workflows are running).
- `internal/api/types.go` — Added `InvestigateResponse` struct to capture `workflowId` from `POST /investigate/single`.

**lac-repo-swarm (Python worker):**

- `src/investigate_worker.py` — Reads `REPOSWARM_PARALLEL` env var and passes `max_concurrent_activities` / `max_concurrent_workflow_task_polls` to the Temporal `Worker()` constructor. Default (unset/0) = unlimited, preserving cloud behaviour.
- `.env.example` — Documented `REPOSWARM_PARALLEL` with note that it is managed by the CLI.
