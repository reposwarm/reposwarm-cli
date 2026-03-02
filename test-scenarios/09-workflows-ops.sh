#!/usr/bin/env bash
# Scenario 09: Workflow operations — list, status, history, prune, cancel
source "$(dirname "$0")/_common.sh"
scenario "09 — Workflow Operations"
wait_for_api

# ── List workflows ──
step "Workflow list"
WF_LIST=$($CLI wf list --json 2>&1)
assert_json_valid "wf list --json valid" "$WF_LIST"

WF_HUMAN=$($CLI wf list 2>&1)
assert_exit_0 "wf list human succeeds" $CLI wf list

# ── List with status filter ──
step "Workflow list with filters"
WF_RUNNING=$($CLI wf list --status running --json 2>&1) || true
assert_json_valid "wf list --status running valid" "$WF_RUNNING"

WF_COMPLETED=$($CLI wf list --status completed --json 2>&1) || true
assert_json_valid "wf list --status completed valid" "$WF_COMPLETED"

# ── Check if any workflows exist ──
HAS_WF=$(echo "$WF_LIST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
wfs = d if isinstance(d, list) else d.get('data', d.get('workflows', []))
print('yes' if len(wfs) > 0 else 'no')" 2>/dev/null || echo "no")

if [ "$HAS_WF" = "yes" ]; then
  WF_ID=$(echo "$WF_LIST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
wfs = d if isinstance(d, list) else d.get('data', d.get('workflows', []))
print(wfs[0].get('workflowId', ''))" 2>/dev/null)

  if [ -n "$WF_ID" ]; then
    step "Workflow status for $WF_ID"
    WF_STATUS=$($CLI wf status "$WF_ID" --json 2>&1)
    assert_json_valid "wf status --json valid" "$WF_STATUS"

    WF_STATUS_V=$($CLI wf status "$WF_ID" -v --json 2>&1)
    assert_json_valid "wf status -v --json valid" "$WF_STATUS_V"

    step "Workflow history for $WF_ID"
    WF_HIST=$($CLI wf history "$WF_ID" --json 2>&1)
    assert_json_valid "wf history --json valid" "$WF_HIST"
  fi
else
  skip "No workflows — skipping status/history tests"
fi

# ── Prune (dry run essentially — only cleans old stuff) ──
step "Workflow prune"
PRUNE=$($CLI wf prune --older-than 30d --json 2>&1) || true
assert_json_valid "wf prune --json valid" "$PRUNE"

# ── Cancel non-existent (should error gracefully) ──
step "Cancel non-existent workflow"
assert_exit_nonzero "Cancel missing wf fails" $CLI wf cancel "nonexistent-wf-id-12345"

# ── Terminate non-existent ──
step "Terminate non-existent workflow"
assert_exit_nonzero "Terminate missing wf fails" $CLI wf terminate "nonexistent-wf-id-12345"

summary
