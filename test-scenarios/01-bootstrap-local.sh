#!/usr/bin/env bash
# Scenario 01: Full local bootstrap — reposwarm new --local
# This is THE big test. Fresh machine → running stack.
source "$(dirname "$0")/_common.sh"
scenario "01 — Bootstrap Local Installation"

# ── Pre-checks ──
step "Verifying clean state"
assert_exit_0 "reposwarm binary exists" which "$CLI"
assert_exit_nonzero "No existing config" test -f "$HOME/.reposwarm/config.json"

# ── Bootstrap ──
step "Running: reposwarm new --local --non-interactive"
BOOTSTRAP_OUTPUT=$($CLI new --local --non-interactive 2>&1) || true
assert_contains "Bootstrap started" "$BOOTSTRAP_OUTPUT" "(temporal|cloning|installing|starting)"

# ── Verify services came up ──
wait_for_temporal 300
wait_for_api 60

# ── Verify config was created ──
assert_exit_0 "Config file exists" test -f "$HOME/.reposwarm/config.json"
CONFIG=$($CLI config show --json 2>&1)
assert_json_valid "Config is valid JSON" "$CONFIG"
assert_json_key "Config has apiUrl" "$CONFIG" "apiUrl"

# ── Verify health ──
HEALTH=$($CLI status --json 2>&1)
assert_json_valid "Status is valid JSON" "$HEALTH"
assert_contains "API connected" "$HEALTH" "connected.*true|healthy"

# ── Verify services visible ──
SERVICES=$($CLI services --json 2>&1)
assert_json_valid "Services JSON valid" "$SERVICES"
assert_contains "API service listed" "$SERVICES" "api"
assert_contains "Temporal service listed" "$SERVICES" "temporal"

summary
