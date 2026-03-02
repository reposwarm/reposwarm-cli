#!/usr/bin/env bash
# Scenario 12: Workers inspection via API
source "$(dirname "$0")/_common.sh"
scenario "12 — Workers Inspection"
wait_for_api

# ── Workers list ──
step "Workers list"
WL=$($CLI workers list --json 2>&1)
assert_json_valid "workers list --json valid" "$WL"
assert_contains "Has workers array" "$WL" "workers"
assert_contains "Has total count" "$WL" "total"
assert_contains "Has healthy count" "$WL" "healthy"

WL_HUMAN=$($CLI workers list 2>&1)
assert_contains "Human shows table" "$WL_HUMAN" "Name|Status|Queue"

# ── Workers list verbose ──
step "Workers list verbose"
WLV=$($CLI workers list -v --json 2>&1)
assert_json_valid "workers list -v --json valid" "$WLV"

WLV_HUMAN=$($CLI workers list -v 2>&1)
assert_contains "Verbose shows PID/Host" "$WLV_HUMAN" "PID|Host"

# ── Workers show ──
step "Workers show"
# Get first worker name
WNAME=$(echo "$WL" | python3 -c "
import sys,json
d=json.load(sys.stdin)
w = d.get('workers', d.get('data', {}).get('workers', []))
print(w[0]['name'] if w else '')" 2>/dev/null || echo "")

if [ -z "$WNAME" ]; then
  skip "No workers found — skipping show test"
else
  SHOW=$($CLI workers show "$WNAME" --json 2>&1)
  assert_json_valid "workers show --json valid" "$SHOW"
  assert_contains "Shows worker name" "$SHOW" "$WNAME"
  assert_contains "Shows env section" "$SHOW" "worker"

  SHOW_HUMAN=$($CLI workers show "$WNAME" 2>&1)
  assert_contains "Human shows details" "$SHOW_HUMAN" "Status|Queue|Environment"

  # ── Show with --no-logs ──
  step "Workers show --no-logs"
  SHOW_NL=$($CLI workers show "$WNAME" --no-logs --json 2>&1)
  assert_json_valid "workers show --no-logs valid" "$SHOW_NL"
fi

# ── Show non-existent worker ──
step "Show missing worker"
assert_exit_nonzero "Missing worker fails" $CLI workers show "phantom-worker-99"

summary
