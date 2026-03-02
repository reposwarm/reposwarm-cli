#!/usr/bin/env bash
# Scenario 07: Logs, services, restart/stop/start
source "$(dirname "$0")/_common.sh"
scenario "07 — Logs & Service Lifecycle"
wait_for_api

# ── Services ──
step "List services"
SVC=$($CLI services --json 2>&1)
assert_json_valid "services --json valid" "$SVC"
assert_contains "Shows api" "$SVC" "api"
assert_contains "Shows worker" "$SVC" "worker"
assert_contains "Shows temporal" "$SVC" "temporal"

SVC_HUMAN=$($CLI services 2>&1)
assert_contains "Human shows status" "$SVC_HUMAN" "running|stopped"

# ── Logs ──
step "View service logs"
LOGS=$($CLI logs worker -n 10 --json 2>&1)
assert_json_valid "logs --json valid" "$LOGS"

LOGS_API=$($CLI logs api -n 5 --json 2>&1)
assert_json_valid "API logs --json valid" "$LOGS_API"

# ── Logs (all services) ──
step "View all logs"
LOGS_ALL=$($CLI logs -n 5 2>&1)
# May or may not have content, but should not error
assert_exit_0 "logs (all) succeeds" $CLI logs -n 5

# ── Stop worker ──
step "Stop worker"
STOP=$($CLI stop worker --json 2>&1) || true
assert_json_valid "stop --json valid" "$STOP"
assert_contains "Stop response" "$STOP" "stopped|not_found"

# ── Start worker ──
step "Start worker"
START=$($CLI start worker --json 2>&1) || true
assert_json_valid "start --json valid" "$START"

# Wait briefly
sleep 3

# ── Restart worker ──
step "Restart worker"
RESTART=$($CLI restart worker --json 2>&1) || true
assert_json_valid "restart --json valid" "$RESTART"

# ── Restart all ──
step "Restart all services"
RESTART_ALL=$($CLI restart --json 2>&1) || true
assert_json_valid "restart all --json valid" "$RESTART_ALL"

# Wait for API to come back
sleep 5
wait_for_api 30

summary
