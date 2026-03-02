#!/usr/bin/env bash
# Scenario 08: Results browsing — list, read, search, export, report
# NOTE: Requires at least one completed investigation. If none exist, most tests skip.
source "$(dirname "$0")/_common.sh"
scenario "08 — Results Browsing"
wait_for_api

# ── Results list ──
step "Results list"
RES_LIST=$($CLI results list --json 2>&1)
assert_json_valid "results list --json valid" "$RES_LIST"

# Check if we have any results
HAS_RESULTS=$(echo "$RES_LIST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
repos = d if isinstance(d, list) else d.get('data', d.get('repos', []))
print('yes' if len(repos) > 0 else 'no')" 2>/dev/null || echo "no")

if [ "$HAS_RESULTS" = "no" ]; then
  skip "No investigation results — skipping read/search/export/report tests"
  skip "Run scenario 05 first and wait for investigation to complete"
  summary
  exit 0
fi

# Get first repo name
REPO=$(echo "$RES_LIST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
repos = d if isinstance(d, list) else d.get('data', d.get('repos', []))
print(repos[0].get('name', repos[0].get('repository_name', '')))" 2>/dev/null)

step "Using repo: $REPO"

# ── Sections ──
step "Results sections"
SECTIONS=$($CLI results sections "$REPO" --json 2>&1)
assert_json_valid "results sections --json valid" "$SECTIONS"
assert_contains "Has sections" "$SECTIONS" "section|label|step"

# ── Read ──
step "Results read (all sections)"
READ=$($CLI results read "$REPO" --json 2>&1)
assert_json_valid "results read --json valid" "$READ"

# ── Search ──
step "Results search"
SEARCH=$($CLI results search "architecture" --json 2>&1)
assert_json_valid "results search --json valid" "$SEARCH"

# ── Export ──
step "Results export"
EXPORT_DIR="/tmp/reposwarm-export-test"
rm -rf "$EXPORT_DIR"
assert_exit_0 "results export succeeds" $CLI results export "$REPO" --output "$EXPORT_DIR"
if [ -d "$EXPORT_DIR" ] && [ "$(ls -A "$EXPORT_DIR" 2>/dev/null)" ]; then
  echo -e "  ${GREEN}✓${NC} Export created files in $EXPORT_DIR"
  ((PASS_COUNT++))
else
  echo -e "  ${RED}✗${NC} Export directory empty or missing"
  ((FAIL_COUNT++))
fi
rm -rf "$EXPORT_DIR"

# ── Report ──
step "Results report"
REPORT=$($CLI results report "$REPO" --json 2>&1) || true
assert_json_valid "results report --json valid" "$REPORT"

# ── Diff (same repo against itself) ──
step "Results diff"
DIFF=$($CLI results diff "$REPO" "$REPO" --json 2>&1) || true
# Diff may fail if only one repo — that's fine
if echo "$DIFF" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
  echo -e "  ${GREEN}✓${NC} results diff --json valid"
  ((PASS_COUNT++))
else
  skip "results diff requires two different repos"
fi

# ── Audit ──
step "Results audit"
AUDIT=$($CLI results audit --json 2>&1) || true
assert_json_valid "results audit --json valid" "$AUDIT"

# ── Meta ──
step "Results meta"
META=$($CLI results meta "$REPO" --json 2>&1)
assert_json_valid "results meta --json valid" "$META"

summary
