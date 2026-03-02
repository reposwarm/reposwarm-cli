#!/usr/bin/env bash
# Scenario 04: Full repo CRUD lifecycle
source "$(dirname "$0")/_common.sh"
scenario "04 — Repo Management"
wait_for_api

TEST_REPO="test-scenario-repo"
TEST_URL="https://github.com/expressjs/express"

# ── Add ──
step "Add a repo"
assert_exit_0 "repos add succeeds" $CLI repos add "$TEST_REPO" --url "$TEST_URL"

# ── List ──
step "List repos"
LIST=$($CLI repos list 2>&1)
assert_contains "Repo appears in list" "$LIST" "$TEST_REPO"

LIST_JSON=$($CLI repos list --json 2>&1)
assert_json_valid "repos list --json valid" "$LIST_JSON"
assert_contains "Repo in JSON" "$LIST_JSON" "$TEST_REPO"

# ── Show ──
step "Show repo detail"
SHOW=$($CLI repos show "$TEST_REPO" 2>&1)
assert_contains "Shows repo name" "$SHOW" "$TEST_REPO"
assert_contains "Shows URL" "$SHOW" "express"

SHOW_JSON=$($CLI repos show "$TEST_REPO" --json 2>&1)
assert_json_valid "repos show --json valid" "$SHOW_JSON"

# ── Disable ──
step "Disable repo"
assert_exit_0 "repos disable succeeds" $CLI repos disable "$TEST_REPO"
SHOW2=$($CLI repos show "$TEST_REPO" --json 2>&1)
assert_contains "Repo is disabled" "$SHOW2" "false"

# ── Enable ──
step "Enable repo"
assert_exit_0 "repos enable succeeds" $CLI repos enable "$TEST_REPO"

# ── Remove ──
step "Remove repo"
assert_exit_0 "repos remove succeeds" $CLI repos remove "$TEST_REPO" --yes

LIST_AFTER=$($CLI repos list --json 2>&1)
# Verify repo is gone (may still show other repos)
if echo "$LIST_AFTER" | grep -q "$TEST_REPO"; then
  echo -e "  ${RED}✗${NC} Repo still present after remove"
  ((FAIL_COUNT++))
else
  echo -e "  ${GREEN}✓${NC} Repo removed successfully"
  ((PASS_COUNT++))
fi

# ── Error: show non-existent ──
step "Error handling"
assert_exit_nonzero "Show missing repo fails" $CLI repos show "nonexistent-repo-xyz"

summary
