#!/usr/bin/env bash
# Scenario 06: Worker environment management via API
source "$(dirname "$0")/_common.sh"
scenario "06 — Worker Environment (via API)"
wait_for_api

# ── List env ──
step "List worker env"
ENV_LIST=$($CLI config worker-env list --json 2>&1)
assert_json_valid "worker-env list --json valid" "$ENV_LIST"
assert_contains "Shows entries" "$ENV_LIST" "entries"
assert_contains "Shows known vars" "$ENV_LIST" "ANTHROPIC_API_KEY|GITHUB_TOKEN"

# ── List with --reveal ──
step "List worker env with --reveal"
ENV_REVEAL=$($CLI config worker-env list --reveal --json 2>&1)
assert_json_valid "worker-env list --reveal valid" "$ENV_REVEAL"

# ── Set a test var ──
step "Set a test env var"
assert_exit_0 "Set TEST_SCENARIO_VAR" $CLI config worker-env set TEST_SCENARIO_VAR "hello-test-123"

# ── Verify it appears ──
ENV_AFTER=$($CLI config worker-env list --reveal --json 2>&1)
assert_contains "Test var appears" "$ENV_AFTER" "TEST_SCENARIO_VAR"
assert_contains "Test var value" "$ENV_AFTER" "hello-test-123"

# ── Unset ──
step "Unset the test var"
assert_exit_0 "Unset TEST_SCENARIO_VAR" $CLI config worker-env unset TEST_SCENARIO_VAR

ENV_FINAL=$($CLI config worker-env list --reveal --json 2>&1)
if echo "$ENV_FINAL" | grep -q "hello-test-123"; then
  echo -e "  ${RED}✗${NC} Var still present after unset"
  ((FAIL_COUNT++))
else
  echo -e "  ${GREEN}✓${NC} Var removed successfully"
  ((PASS_COUNT++))
fi

# ── Human-readable output ──
step "Human-readable format"
ENV_HUMAN=$($CLI config worker-env list 2>&1)
assert_contains "Shows table headers" "$ENV_HUMAN" "Variable|Source|Value"

summary
