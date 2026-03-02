#!/usr/bin/env bash
# Scenario 10: All commands with --json and --for-agent flags
source "$(dirname "$0")/_common.sh"
scenario "10 — JSON & Agent Mode Output"
wait_for_api

COMMANDS=(
  "status"
  "services"
  "doctor"
  "repos list"
  "workers list"
  "wf list"
  "config show"
)

# ── JSON mode ──
step "Testing --json output for all commands"
for cmd in "${COMMANDS[@]}"; do
  OUTPUT=$($CLI $cmd --json 2>&1) || true
  assert_json_valid "$cmd --json" "$OUTPUT"
done

# ── Agent mode ──
step "Testing --for-agent output for all commands"
for cmd in "${COMMANDS[@]}"; do
  OUTPUT=$($CLI $cmd --for-agent 2>&1) || true
  # Agent mode should not have ANSI escape codes
  if echo "$OUTPUT" | grep -qP '\033\['; then
    echo -e "  ${RED}✗${NC} $cmd --for-agent has ANSI codes"
    ((FAIL_COUNT++))
  else
    echo -e "  ${GREEN}✓${NC} $cmd --for-agent is plain text"
    ((PASS_COUNT++))
  fi
done

# ── Combined flags ──
step "Testing --json + --verbose"
for cmd in "status" "wf list"; do
  OUTPUT=$($CLI $cmd --json --verbose 2>&1) || true
  assert_json_valid "$cmd --json --verbose" "$OUTPUT"
done

summary
