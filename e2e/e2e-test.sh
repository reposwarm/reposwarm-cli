#!/usr/bin/env bash
# RepoSwarm E2E Test — fresh install → investigate → ask
# Runs on a clean Linux machine with Docker installed.
# Required env vars: GITHUB_TOKEN (GitHub PAT with repo access)
# Optional: AWS_REGION (default: us-east-1), ARCH_HUB_REPO (default: reposwarm/e2e-arch-hub)
set -euo pipefail

# ── Config ──────────────────────────────────────────────────────────────────
ARCH_HUB_REPO="${ARCH_HUB_REPO:-reposwarm/e2e-arch-hub}"
ARCH_HUB_BASE_URL="https://github.com/${ARCH_HUB_REPO%/*}"
ARCH_HUB_REPO_NAME="${ARCH_HUB_REPO#*/}"
AWS_REGION="${AWS_REGION:-us-east-1}"
ANTHROPIC_MODEL="${ANTHROPIC_MODEL:-us.anthropic.claude-sonnet-4-20250514-v1:0}"
INVESTIGATE_REPO="${INVESTIGATE_REPO:-is-odd}"
INVESTIGATE_URL="${INVESTIGATE_URL:-https://github.com/jonschlinkert/is-odd}"
TIMEOUT_INVESTIGATE="${TIMEOUT_INVESTIGATE:-600}"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ── Helpers ─────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; BLUE='\033[0;34m'; NC='\033[0m'
log_info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
log_pass()  { echo -e "${GREEN}[PASS]${NC} $*"; TESTS_RUN=$((TESTS_RUN + 1)); TESTS_PASSED=$((TESTS_PASSED + 1)); }
log_fail()  { echo -e "${RED}[FAIL]${NC} $*"; TESTS_RUN=$((TESTS_RUN + 1)); TESTS_FAILED=$((TESTS_FAILED + 1)); FAILED_NAMES+=("$*"); }

assert_contains() {
  local haystack="$1" needle="$2" label="$3"
  if echo "$haystack" | grep -qi "$needle"; then
    log_pass "$label"
  else
    log_fail "$label (expected '$needle' not found)"
  fi
}

assert_exit_zero() {
  local label="$1"; shift
  local output
  if output=$("$@" 2>&1); then
    log_pass "$label"
    echo "$output"  # return output for further checks
  else
    log_fail "$label (exit code $?)"
    echo "$output"
  fi
}

wait_for() {
  local timeout="$1" interval="$2" desc="$3"; shift 3
  local cmd="$*"
  local elapsed=0
  log_info "Waiting for: $desc (timeout=${timeout}s)"
  while [ "$elapsed" -lt "$timeout" ]; do
    if eval "$cmd" >/dev/null 2>&1; then
      log_pass "$desc"
      return 0
    fi
    sleep "$interval"
    elapsed=$((elapsed + interval))
  done
  log_fail "$desc (timed out after ${timeout}s)"
  return 1
}

cleanup() {
  log_info "Cleaning up..."
  if [ -f "$HOME/.reposwarm/temporal/docker-compose.yml" ]; then
    docker compose -f "$HOME/.reposwarm/temporal/docker-compose.yml" down -v 2>/dev/null || true
  fi
  docker rm -f $(docker ps -aq --filter "name=reposwarm-*") 2>/dev/null || true
  sudo rm -rf "$HOME/.reposwarm" "$HOME/.config/ask" "$HOME/.ask"
  sudo rm -f /usr/local/bin/reposwarm "$HOME/.local/bin/reposwarm"
  sudo rm -f /usr/local/bin/ask "$HOME/.local/bin/ask"
  log_info "Cleanup done"
}

summary() {
  echo ""
  echo "════════════════════════════════════════════"
  echo "  RESULTS: $TESTS_PASSED/$TESTS_RUN passed, $TESTS_FAILED failed"
  echo "════════════════════════════════════════════"
  if [ ${#FAILED_NAMES[@]} -gt 0 ]; then
    echo "  Failed tests:"
    for name in "${FAILED_NAMES[@]}"; do
      echo "    ✗ $name"
    done
  fi
  echo ""
  [ "$TESTS_FAILED" -eq 0 ]
}

# ── Preflight checks ───────────────────────────────────────────────────────
log_info "Checking prerequisites..."
[ -z "${GITHUB_TOKEN:-}" ] && { echo "ERROR: GITHUB_TOKEN not set"; exit 1; }
command -v docker >/dev/null || { echo "ERROR: docker not installed"; exit 1; }
command -v curl >/dev/null || { echo "ERROR: curl not installed"; exit 1; }

# ── Cleanup ─────────────────────────────────────────────────────────────────
cleanup

# ════════════════════════════════════════════════════════════════════════════
# SCENARIO 1: Install CLI + create local workspace
# ════════════════════════════════════════════════════════════════════════════
log_info "═══ SCENARIO 1: INSTALL & SETUP ═══"

# 1a. Install RepoSwarm CLI
log_info "1a. Installing RepoSwarm CLI..."
curl -fsSL https://raw.githubusercontent.com/reposwarm/reposwarm-cli/main/install.sh | sh 2>&1
export PATH="$HOME/.local/bin:$PATH"
assert_exit_zero "CLI installed" command -v reposwarm

cli_version=$(reposwarm --version 2>&1)
assert_contains "$cli_version" "reposwarm version" "CLI version reported"

# 1b. Install Ask CLI
log_info "1b. Installing Ask CLI..."
curl -fsSL https://raw.githubusercontent.com/reposwarm/ask-cli/main/install.sh | sh 2>&1
assert_exit_zero "Ask CLI installed" command -v ask

# 1c. Create local workspace
# Known issue: reposwarm new --local may fail with API unhealthy because
# DynamoDB Local's SQLite can't write to Docker volumes on ARM64.
# Workaround: let it fail, patch compose to use -inMemory, restart.
log_info "1c. Creating local RepoSwarm workspace..."
new_output=$(reposwarm new --local --for-agent 2>&1 || true)
if echo "$new_output" | grep -qi "RepoSwarm local environment is running"; then
  log_pass "Workspace created (first try)"
else
  log_info "  new --local failed (expected on ARM64). Applying DDB Local -inMemory fix..."
  COMPOSE_FILE="$HOME/.reposwarm/temporal/docker-compose.yml"
  if [ -f "$COMPOSE_FILE" ]; then
    # Stop everything
    docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
    # Patch DynamoDB Local to use in-memory mode (fixes SQLite volume permission issue)
    sed -i 's|"-jar", "DynamoDBLocal.jar", "-sharedDb", "-dbPath", "/home/dynamodblocal/data"|"-jar", "DynamoDBLocal.jar", "-sharedDb", "-inMemory"|' "$COMPOSE_FILE"
    # Also add dummy AWS credentials for API so SDK doesn't try IMDS
    sed -i '/REPOSWARM_INSTALL_DIR/a\      - AWS_ACCESS_KEY_ID=local\n      - AWS_SECRET_ACCESS_KEY=local' "$COMPOSE_FILE"
    # Restart with fix
    docker compose -f "$COMPOSE_FILE" up -d 2>&1
    log_pass "Workspace created (with DDB Local -inMemory fix)"
  else
    log_fail "Workspace creation failed and no compose file found"
  fi
fi

# Extract API token from .env file
API_TOKEN=$(grep API_BEARER_TOKEN "$HOME/.reposwarm/temporal/.env" | cut -d= -f2)
[ -n "$API_TOKEN" ] && log_pass "API token generated" || log_fail "API token missing from .env"

# Write token to config (workaround: new --local doesn't auto-configure it)
if [ -f "$HOME/.reposwarm/config.json" ]; then
  tmpf=$(mktemp)
  jq --arg t "$API_TOKEN" '.apiToken = $t' "$HOME/.reposwarm/config.json" > "$tmpf" && mv "$tmpf" "$HOME/.reposwarm/config.json"
fi

# 1d. Wait for API to be healthy, then verify containers
log_info "1d. Waiting for API health..."
wait_for 120 10 "API container healthy" \
  "docker inspect reposwarm-api --format '{{.State.Health.Status}}' 2>/dev/null | grep -q healthy"

log_info "1d. Verifying containers..."
for container in reposwarm-api reposwarm-temporal reposwarm-postgres reposwarm-dynamodb; do
  if docker ps --format "{{.Names}}" | grep -q "^${container}$"; then
    log_pass "Container $container running"
  else
    log_fail "Container $container not running"
  fi
done

# 1e. Verify API health
log_info "1e. Checking API health..."
health=$(curl -sf http://localhost:3000/v1/health 2>&1 || echo '{}')
assert_contains "$health" '"status":"healthy"' "API healthy"
assert_contains "$health" '"dynamodb":{"connected":true}' "DynamoDB connected"

# 1f. Configure provider (Bedrock)
log_info "1f. Configuring Bedrock provider..."
provider_output=$(reposwarm config provider setup bedrock \
  --region "$AWS_REGION" \
  --model "$ANTHROPIC_MODEL" \
  --for-agent 2>&1)
assert_contains "$provider_output" "bedrock" "Bedrock provider configured"

# 1g. Configure worker environment
log_info "1g. Setting worker environment..."
reposwarm config worker-env set GITHUB_TOKEN "$GITHUB_TOKEN" --for-agent 2>&1
reposwarm config worker-env set ARCH_HUB_BASE_URL "$ARCH_HUB_BASE_URL" --for-agent 2>&1
reposwarm config worker-env set ARCH_HUB_REPO_NAME "$ARCH_HUB_REPO_NAME" --for-agent 2>&1
reposwarm config worker-env set ANTHROPIC_MODEL "$ANTHROPIC_MODEL" --for-agent 2>&1
reposwarm config worker-env set CLAUDE_CODE_USE_BEDROCK 1 --for-agent 2>&1
reposwarm config worker-env set AWS_REGION "$AWS_REGION" --restart --for-agent 2>&1
log_pass "Worker environment configured"

# Wait for worker to be healthy after restart
wait_for 60 5 "Worker healthy after restart" \
  "docker ps --format '{{.Names}} {{.Status}}' | grep 'reposwarm-worker' | grep -q 'healthy'"

# 1h. Preflight check
log_info "1h. Running preflight..."
preflight_output=$(reposwarm preflight --for-agent 2>&1)
assert_contains "$preflight_output" "Ready to investigate" "Preflight passed"

# 1i. Doctor check
log_info "1i. Running doctor..."
doctor_output=$(reposwarm doctor --for-agent 2>&1)
doctor_fails=$(echo "$doctor_output" | grep -c "\[FAIL\]" || echo "0")
if [ "$doctor_fails" = "0" ]; then
  log_pass "Doctor: zero failures"
else
  log_fail "Doctor: $doctor_fails failures"
fi

# ════════════════════════════════════════════════════════════════════════════
# SCENARIO 2: Add repo + investigate + check results
# ════════════════════════════════════════════════════════════════════════════
log_info "═══ SCENARIO 2: INVESTIGATE ═══"

# 2a. Add repository
log_info "2a. Adding $INVESTIGATE_REPO..."
add_output=$(reposwarm repos add "$INVESTIGATE_REPO" \
  --url "$INVESTIGATE_URL" \
  --source GitHub \
  --for-agent 2>&1)
assert_contains "$add_output" "Added repository $INVESTIGATE_REPO" "Repository added"

# 2b. Verify repo listed
log_info "2b. Listing repos..."
list_output=$(reposwarm repos list --for-agent 2>&1)
assert_contains "$list_output" "$INVESTIGATE_REPO" "Repo in list"

# 2c. Start investigation
log_info "2c. Starting investigation..."
inv_output=$(reposwarm investigate "$INVESTIGATE_REPO" --for-agent 2>&1)
assert_contains "$inv_output" "Investigation started for $INVESTIGATE_REPO" "Investigation started"

# 2d. Wait for investigation to complete
wait_for "$TIMEOUT_INVESTIGATE" 15 "Investigation completed" \
  "reposwarm workflows list --for-agent 2>&1 | grep '$INVESTIGATE_REPO' | grep -q 'Completed'"

# 2e. Check results
log_info "2e. Checking results..."
results_output=$(reposwarm results show "$INVESTIGATE_REPO" --for-agent 2>&1)
assert_contains "$results_output" "Results — $INVESTIGATE_REPO" "Results header present"
assert_contains "$results_output" "hl_overview" "hl_overview section present"
assert_contains "$results_output" "dependencies" "dependencies section present"

# 2f. Read a section
log_info "2f. Reading hl_overview..."
overview=$(reposwarm results read "$INVESTIGATE_REPO" hl_overview --for-agent 2>&1)
if [ ${#overview} -gt 100 ]; then
  log_pass "Overview has content (${#overview} chars)"
else
  log_fail "Overview too short (${#overview} chars)"
fi

# ════════════════════════════════════════════════════════════════════════════
# SCENARIO 3: Ask CLI — read results from arch-hub
# ════════════════════════════════════════════════════════════════════════════
log_info "═══ SCENARIO 3: ASK CLI ═══"

# 3a. Configure ask CLI
log_info "3a. Configuring ask CLI..."
mkdir -p "$HOME/.config/ask"
cat > "$HOME/.config/ask/config.json" << ASKEOF
{
  "askbox_url": "http://localhost:8082",
  "arch_hub": {
    "base_url": "$ARCH_HUB_BASE_URL",
    "repo_name": "$ARCH_HUB_REPO_NAME"
  },
  "provider": {
    "type": "bedrock",
    "region": "$AWS_REGION",
    "model": "$ANTHROPIC_MODEL"
  },
  "github_token_env": "GITHUB_TOKEN"
}
ASKEOF
log_pass "Ask CLI configured"

# 3b. List results from arch-hub (doesn't need askbox)
log_info "3b. Listing architecture results..."
results_list=$(ask results list --for-agent 2>&1)
assert_contains "$results_list" "$INVESTIGATE_REPO" "Investigated repo in ask results"

# 3c. Read a specific section via ask
log_info "3c. Reading section via ask results..."
ask_overview=$(ask results read "$INVESTIGATE_REPO" hl_overview --for-agent 2>&1)
if [ ${#ask_overview} -gt 100 ]; then
  log_pass "Ask results read has content (${#ask_overview} chars)"
else
  log_fail "Ask results read too short (${#ask_overview} chars)"
fi

# 3d. Start askbox for question flow
log_info "3d. Starting askbox..."
export GITHUB_TOKEN="$GITHUB_TOKEN"
ask up --for-agent 2>&1 || true
sleep 10

# Check if askbox is running
askbox_status=$(ask status --for-agent 2>&1 || echo "not running")
if echo "$askbox_status" | grep -qi "healthy\|running"; then
  log_pass "Askbox is running"

  # 3e. Ask a question
  log_info "3e. Asking about $INVESTIGATE_REPO..."
  answer=$(timeout 120 ask ask "What does $INVESTIGATE_REPO do?" --for-agent 2>&1 || echo "timeout")
  if [ ${#answer} -gt 50 ]; then
    log_pass "Question answered (${#answer} chars)"
  else
    log_fail "Question answer too short: $answer"
  fi
else
  log_fail "Askbox not running — skipping question test"
  log_info "  (askbox status: $askbox_status)"
fi

# ════════════════════════════════════════════════════════════════════════════
# SCENARIO 4: Workflows & diagnostics
# ════════════════════════════════════════════════════════════════════════════
log_info "═══ SCENARIO 4: DIAGNOSTICS ═══"

# 4a. Workflows list
log_info "4a. Listing workflows..."
wf_output=$(reposwarm workflows list --for-agent 2>&1)
assert_contains "$wf_output" "$INVESTIGATE_REPO" "Investigation in workflow list"
assert_contains "$wf_output" "Completed" "Workflow completed"

# 4b. Config show
log_info "4b. Checking config..."
config_output=$(reposwarm config show --for-agent 2>&1)
assert_contains "$config_output" "bedrock" "Bedrock in config"

# 4c. Workers list
log_info "4c. Listing workers..."
workers_output=$(reposwarm workers list --for-agent 2>&1)
assert_contains "$workers_output" "healthy" "Worker healthy"

# ════════════════════════════════════════════════════════════════════════════
# CLEANUP & SUMMARY
# ════════════════════════════════════════════════════════════════════════════
log_info "═══ CLEANUP ═══"
ask down --for-agent 2>&1 || true
cleanup
summary
