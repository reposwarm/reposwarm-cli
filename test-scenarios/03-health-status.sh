#!/usr/bin/env bash
# Scenario 03: Health, status, doctor, preflight, services
source "$(dirname "$0")/_common.sh"
scenario "03 — Health & Status"
wait_for_api

# ── Status ──
step "Status command"
assert_exit_0 "status succeeds" $CLI status
STATUS=$($CLI status --json 2>&1)
assert_json_valid "status --json valid" "$STATUS"
assert_json_key "Has status field" "$STATUS" "status"

# ── Doctor ──
step "Doctor command"
DOCTOR=$($CLI doctor 2>&1)
assert_exit_0 "doctor succeeds" $CLI doctor
assert_contains "Doctor checks API" "$DOCTOR" "(api|temporal|worker|check)"

DOCTOR_JSON=$($CLI doctor --json 2>&1)
assert_json_valid "doctor --json valid" "$DOCTOR_JSON"

# ── Preflight ──
step "Preflight command"
PREFLIGHT=$($CLI preflight 2>&1) || true
# Preflight may fail if worker env isn't set — that's OK, we're testing it runs
assert_contains "Preflight runs checks" "$PREFLIGHT" "(api|temporal|worker|check|pass|fail|warn)"

PREFLIGHT_JSON=$($CLI preflight --json 2>&1) || true
assert_json_valid "preflight --json valid" "$PREFLIGHT_JSON"

# ── Services ──
step "Services command"
assert_exit_0 "services succeeds" $CLI services
SERVICES=$($CLI services --json 2>&1)
assert_json_valid "services --json valid" "$SERVICES"
assert_contains "Shows api service" "$SERVICES" "api"

# ── Version ──
step "Version command"
assert_exit_0 "version succeeds" $CLI version
VERSION=$($CLI version 2>&1)
assert_contains "Has version number" "$VERSION" "[0-9]+\.[0-9]+"

summary
