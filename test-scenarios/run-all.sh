#!/usr/bin/env bash
# Run all RepoSwarm CLI E2E test scenarios
set -uo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'; BOLD='\033[1m'

echo -e "${BOLD}${CYAN}"
echo "╔══════════════════════════════════════════════╗"
echo "║   RepoSwarm CLI — E2E Test Suite             ║"
echo "╚══════════════════════════════════════════════╝"
echo -e "${NC}"

TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_SKIP=0
FAILED_SCENARIOS=()
PASSED_SCENARIOS=()

# Skip 01 if --skip-bootstrap flag
SKIP_BOOTSTRAP=false
for arg in "$@"; do
  if [ "$arg" = "--skip-bootstrap" ]; then
    SKIP_BOOTSTRAP=true
  fi
done

for scenario in "$DIR"/[0-9]*.sh; do
  name=$(basename "$scenario" .sh)

  if [ "$SKIP_BOOTSTRAP" = true ] && [ "$name" = "01-bootstrap-local" ]; then
    echo -e "${YELLOW}⊘ Skipping $name (--skip-bootstrap)${NC}"
    continue
  fi

  echo -e "\n${BOLD}Running: $name${NC}"
  if bash "$scenario" 2>&1 | tee "/tmp/reposwarm-scenario-${name}.log"; then
    PASSED_SCENARIOS+=("$name")
  else
    FAILED_SCENARIOS+=("$name")
  fi
done

# ── Final Summary ──
echo -e "\n${BOLD}${CYAN}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${CYAN}║   FINAL RESULTS                              ║${NC}"
echo -e "${BOLD}${CYAN}╚══════════════════════════════════════════════╝${NC}"

echo -e "\n  ${GREEN}Passed: ${#PASSED_SCENARIOS[@]}${NC}"
for s in "${PASSED_SCENARIOS[@]}"; do
  echo -e "    ${GREEN}✓${NC} $s"
done

if [ ${#FAILED_SCENARIOS[@]} -gt 0 ]; then
  echo -e "\n  ${RED}Failed: ${#FAILED_SCENARIOS[@]}${NC}"
  for s in "${FAILED_SCENARIOS[@]}"; do
    echo -e "    ${RED}✗${NC} $s"
    echo -e "      Log: /tmp/reposwarm-scenario-${s}.log"
  done
fi

echo ""
if [ ${#FAILED_SCENARIOS[@]} -gt 0 ]; then
  echo -e "  ${RED}${BOLD}SUITE FAILED${NC}"
  exit 1
fi
echo -e "  ${GREEN}${BOLD}ALL SCENARIOS PASSED${NC}"
