#!/usr/bin/env bash
# Scenario 02: Config init/set/show/server lifecycle
source "$(dirname "$0")/_common.sh"
scenario "02 — Config Lifecycle"
wait_for_api

# ── Show current config ──
step "Config show"
OUTPUT=$($CLI config show 2>&1)
assert_exit_0 "config show succeeds" $CLI config show
assert_contains "Shows API URL" "$OUTPUT" "apiUrl|api.*url|localhost"

OUTPUT_JSON=$($CLI config show --json 2>&1)
assert_json_valid "config show --json valid" "$OUTPUT_JSON"

# ── Set values ──
step "Config set"
assert_exit_0 "Set custom apiUrl" $CLI config set apiUrl http://localhost:3000/v1
assert_exit_0 "Set custom apiToken" $CLI config set apiToken test-token-123

OUTPUT=$($CLI config show --json 2>&1)
assert_contains "apiUrl persisted" "$OUTPUT" "localhost:3000"

# ── Server config (reads from API) ──
step "Server config"
SERVER_OUTPUT=$($CLI config server --json 2>&1)
assert_json_valid "config server --json valid" "$SERVER_OUTPUT"

# ── Reset ──
step "Reset config"
assert_exit_0 "Reset apiToken" $CLI config set apiToken ""

summary
