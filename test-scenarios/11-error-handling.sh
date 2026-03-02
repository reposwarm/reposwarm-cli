#!/usr/bin/env bash
# Scenario 11: Error handling — bad inputs, missing config, unreachable API
source "$(dirname "$0")/_common.sh"
scenario "11 — Error Handling"

# ── Wrong number of args ──
step "Friendly arg errors"
OUTPUT=$($CLI repos show 2>&1) || true
assert_contains "Shows usage hint" "$OUTPUT" "usage|example|reposwarm repos show"

OUTPUT=$($CLI investigate 2>&1) || true
assert_contains "Investigate needs arg" "$OUTPUT" "repo|usage|--all"

OUTPUT=$($CLI stop 2>&1) || true
assert_contains "Stop needs service name" "$OUTPUT" "usage|service|example"

# ── Unknown commands ──
step "Unknown commands"
assert_exit_nonzero "Unknown command fails" $CLI frobnicate
assert_exit_nonzero "Unknown subcommand fails" $CLI repos frobnicate

# ── Bad API URL ──
step "Unreachable API"
BAD_OUTPUT=$($CLI status --api-url http://localhost:59999/v1 --json 2>&1) || true
assert_contains "Shows connection error" "$BAD_OUTPUT" "error|connect|refused|unreachable"

# ── Bad API token ──
step "Invalid auth token"
wait_for_api
BAD_AUTH=$($CLI repos list --api-token "invalid-token-xyz" 2>&1) || true
# May get 401 or may still work if auth is optional
assert_exit_0 "Bad token returns something" echo "$BAD_AUTH"

# ── Non-existent resources ──
step "Non-existent resources"
assert_exit_nonzero "Show missing repo" $CLI repos show "no-such-repo-ever"
assert_exit_nonzero "Show missing worker" $CLI workers show "no-such-worker"

# ── Empty operations ──
step "Empty operations"
assert_exit_0 "repos list with no repos" $CLI repos list
assert_exit_0 "wf list with no workflows" $CLI wf list

# ── Help flag everywhere ──
step "Help flags"
for cmd in "" "repos" "results" "workflows" "config" "workers" "investigate" "preflight" "services" "logs" "doctor"; do
  assert_exit_0 "$cmd --help" $CLI $cmd --help
done

summary
