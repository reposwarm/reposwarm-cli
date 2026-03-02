#!/usr/bin/env bash
# Scenario 05: Trigger a single-repo investigation and track it
source "$(dirname "$0")/_common.sh"
scenario "05 — Investigate Single Repo"
wait_for_api

TEST_REPO="is-odd"
TEST_URL="https://github.com/jonschlinkert/is-odd"

# ── Setup: add repo ──
step "Setup: add test repo"
$CLI repos add "$TEST_REPO" --url "$TEST_URL" 2>/dev/null || true

# ── Preflight for this repo ──
step "Preflight with repo"
PREFLIGHT=$($CLI preflight "$TEST_REPO" --json 2>&1) || true
assert_json_valid "preflight --json valid" "$PREFLIGHT"

# ── Investigate with --force (skip preflight) ──
step "Trigger investigation"
INV_OUTPUT=$($CLI investigate "$TEST_REPO" --force --json 2>&1)
assert_json_valid "investigate --json valid" "$INV_OUTPUT"
assert_contains "Workflow started" "$INV_OUTPUT" "workflow|started|running"

# Extract workflow ID
WF_ID=$(echo "$INV_OUTPUT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(d.get('workflowId', d.get('data', {}).get('workflowId', '')))" 2>/dev/null || echo "")

if [ -z "$WF_ID" ]; then
  skip "Could not extract workflow ID — skipping tracking tests"
else
  # ── Workflow status ──
  step "Check workflow status"
  WF_STATUS=$($CLI wf status "$WF_ID" --json 2>&1)
  assert_json_valid "wf status --json valid" "$WF_STATUS"
  assert_contains "Shows status" "$WF_STATUS" "running|completed|failed"

  # ── Workflow list ──
  step "Workflow appears in list"
  WF_LIST=$($CLI wf list --json 2>&1)
  assert_json_valid "wf list --json valid" "$WF_LIST"
  assert_contains "Workflow in list" "$WF_LIST" "$WF_ID"

  # ── Workflow history ──
  step "Workflow history"
  WF_HIST=$($CLI wf history "$WF_ID" --json 2>&1)
  assert_json_valid "wf history --json valid" "$WF_HIST"

  # ── Progress (non-blocking snapshot) ──
  step "Workflow progress"
  PROGRESS=$($CLI wf progress --repo "$TEST_REPO" --json 2>&1) || true
  assert_json_valid "wf progress --json valid" "$PROGRESS"
fi

# ── Investigate --dry-run ──
step "Investigate --dry-run"
DRY=$($CLI investigate "$TEST_REPO" --dry-run --force --json 2>&1)
assert_json_valid "dry-run --json valid" "$DRY"
assert_contains "Dry run mode" "$DRY" "dry.run|preflight|would"

# ── Cleanup ──
step "Cleanup"
if [ -n "$WF_ID" ]; then
  $CLI wf terminate "$WF_ID" 2>/dev/null || true
fi
$CLI repos remove "$TEST_REPO" --yes 2>/dev/null || true

summary
