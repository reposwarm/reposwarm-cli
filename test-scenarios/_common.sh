#!/usr/bin/env bash
# Shared test helpers for RepoSwarm CLI E2E scenarios
set -euo pipefail

# ── Colors ──
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'; BOLD='\033[1m'

# ── State ──
SCENARIO_NAME=""
PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
TEST_LOG="/tmp/reposwarm-test-$(date +%s).log"
CLI="${CLI:-reposwarm}"
API_URL="${API_URL:-http://localhost:3000/v1}"
API_TOKEN="${API_TOKEN:-test-token}"

# ── Functions ──

scenario() {
  SCENARIO_NAME="$1"
  echo -e "\n${BOLD}${CYAN}━━━ Scenario: $1 ━━━${NC}"
}

step() {
  echo -e "  ${CYAN}▸${NC} $1"
}

assert_exit_0() {
  local desc="$1"; shift
  local output
  if output=$("$@" 2>&1); then
    echo -e "  ${GREEN}✓${NC} $desc"
    ((PASS_COUNT++))
    echo "$output" >> "$TEST_LOG"
  else
    local code=$?
    echo -e "  ${RED}✗${NC} $desc (exit $code)"
    echo "    CMD: $*"
    echo "    OUTPUT: $(echo "$output" | tail -5)"
    ((FAIL_COUNT++))
    echo "FAIL: $desc | CMD: $* | OUTPUT: $output" >> "$TEST_LOG"
  fi
}

assert_exit_nonzero() {
  local desc="$1"; shift
  local output
  if output=$("$@" 2>&1); then
    echo -e "  ${RED}✗${NC} $desc (expected failure, got exit 0)"
    ((FAIL_COUNT++))
  else
    echo -e "  ${GREEN}✓${NC} $desc (correctly failed)"
    ((PASS_COUNT++))
  fi
}

assert_contains() {
  local desc="$1"; local output="$2"; local pattern="$3"
  if echo "$output" | grep -qiE "$pattern"; then
    echo -e "  ${GREEN}✓${NC} $desc"
    ((PASS_COUNT++))
  else
    echo -e "  ${RED}✗${NC} $desc (pattern not found: $pattern)"
    echo "    OUTPUT: $(echo "$output" | head -5)"
    ((FAIL_COUNT++))
  fi
}

assert_json_key() {
  local desc="$1"; local output="$2"; local key="$3"
  if echo "$output" | python3 -c "import sys,json; d=json.load(sys.stdin); assert '$key' in str(d)" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} $desc"
    ((PASS_COUNT++))
  else
    echo -e "  ${RED}✗${NC} $desc (key '$key' not in JSON)"
    ((FAIL_COUNT++))
  fi
}

assert_json_valid() {
  local desc="$1"; local output="$2"
  if echo "$output" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} $desc"
    ((PASS_COUNT++))
  else
    echo -e "  ${RED}✗${NC} $desc (invalid JSON)"
    ((FAIL_COUNT++))
  fi
}

skip() {
  echo -e "  ${YELLOW}⊘${NC} $1 (skipped)"
  ((SKIP_COUNT++))
}

summary() {
  local total=$((PASS_COUNT + FAIL_COUNT + SKIP_COUNT))
  echo -e "\n${BOLD}━━━ Results: $SCENARIO_NAME ━━━${NC}"
  echo -e "  ${GREEN}✓ $PASS_COUNT passed${NC}  ${RED}✗ $FAIL_COUNT failed${NC}  ${YELLOW}⊘ $SKIP_COUNT skipped${NC}  ($total total)"
  echo -e "  Log: $TEST_LOG"
  if [ "$FAIL_COUNT" -gt 0 ]; then
    echo -e "  ${RED}SCENARIO FAILED${NC}"
    return 1
  fi
  echo -e "  ${GREEN}SCENARIO PASSED${NC}"
}

wait_for_api() {
  local max_wait=${1:-60}
  local waited=0
  step "Waiting for API at $API_URL (max ${max_wait}s)..."
  while [ $waited -lt $max_wait ]; do
    if curl -sf "$API_URL/health" >/dev/null 2>&1; then
      echo -e "  ${GREEN}✓${NC} API is up (${waited}s)"
      return 0
    fi
    sleep 2
    waited=$((waited + 2))
  done
  echo -e "  ${RED}✗${NC} API not reachable after ${max_wait}s"
  return 1
}

wait_for_temporal() {
  local max_wait=${1:-120}
  local waited=0
  step "Waiting for Temporal UI at localhost:8233 (max ${max_wait}s)..."
  while [ $waited -lt $max_wait ]; do
    if curl -sf http://localhost:8233 >/dev/null 2>&1; then
      echo -e "  ${GREEN}✓${NC} Temporal is up (${waited}s)"
      return 0
    fi
    sleep 3
    waited=$((waited + 3))
  done
  echo -e "  ${RED}✗${NC} Temporal not reachable after ${max_wait}s"
  return 1
}
